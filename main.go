package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stratoberry/go-gpsd"
	"tinygo.org/x/bluetooth"
)

var adapter = bluetooth.DefaultAdapter

type LocationData struct {
	Fix       bool
	Latitude  float64
	Longitude float64
	Altitude  float64
	Error     float64
}

var (
	currentLocation LocationData
	locationMu      sync.Mutex
)

// firstSeen tracks the first time each device address was observed.
var firstSeen = make(map[string]time.Time)

func main() {
	must("enable BLE stack", adapter.Enable())

	var gps *gpsd.Session

	// This is cursed, I'm sorry.
	must("gpsd dial", func() error {
		gpsSession, err := gpsd.Dial("localhost:2947")
		gps = gpsSession
		return err
	}())

	tpvFilter := func(r any) {
		report := r.(*gpsd.TPVReport)
		fix := report.Mode >= 2
		locationMu.Lock()
		currentLocation = LocationData{
			Fix:       fix,
			Latitude:  report.Lat,
			Longitude: report.Lon,
			Altitude:  report.Alt,
			Error:     report.Eph,
		}
		locationMu.Unlock()
		fmt.Printf("GPS update: Fix %t Lat %.6f Lon %.6f Alt %.1f m Acc %.1f m\n",
			fix, report.Lat, report.Lon, report.Alt, report.Eph)
	}

	gps.AddFilter("TPV", tpvFilter)

	// Connect to system D-Bus for BlueZ device properties.
	dbusConn, err := dbus.SystemBus()
	must("connect to system dbus", err)

	// Create CSV in /root/loot/wigle-bluetooth/
	must("create loot directory", os.MkdirAll("/root/loot/wigle-bluetooth", 0755))
	csvPath := fmt.Sprintf("/root/loot/wigle-bluetooth/wigle-bluetooth-%s.csv",
		time.Now().UTC().Format("2006-01-02T150405.000000000-0700"))
	csvFile, err := os.Create(csvPath)
	must("create CSV file", err)
	defer csvFile.Close()

	writer := csv.NewWriter(csvFile)
	defer writer.Flush()

	// CSV pre-header
	// Yes, I'm hardcoding the firmware version.
	// I could not figure out for the life of me how to get the Pineapple's
	// firmware version from anywhere on the system and I figured it just wasn't worth it.
	// I'm just going to leave it commented for now until I find a way.
	// writer.Write([]string{
	// 	"WigleWifi-1.6", "appRelease=1.0.7", "model=pineapplepager",
	// 	"release=1.0.7", "device=pineapplepager", "display=na", "board=na",
	// 	"brand=Hak5", "star=Sol", "body=3", "subBody=0",
	// })

	// WiGLE Bluetooth CSV header.
	writer.Write([]string{
		"MAC", "SSID", "AuthMode", "FirstSeen", "Channel",
		"Frequency", "RSSI", "CurrentLatitude", "CurrentLongitude",
		"AltitudeMeters", "AccuracyMeters", "RCOIs", "MfgrId", "Type",
	})
	writer.Flush()

	fmt.Println("Writing to", csvPath)

	gps.Watch()

	err = adapter.Scan(func(adapter *bluetooth.Adapter, device bluetooth.ScanResult) {
		locationMu.Lock()
		loc := currentLocation
		locationMu.Unlock()

		if !loc.Fix {
			fmt.Println("No GPS fix, skipping device:", device.Address.String())
			return
		}

		addr := device.Address.String()
		now := time.Now().UTC()

		// Track first-seen time.
		if _, seen := firstSeen[addr]; !seen {
			firstSeen[addr] = now
		}

		// Get device class from BlueZ over D-Bus.
		deviceClass := getDeviceClass(dbusConn, addr)

		// Build capabilities string.
		capabilities := buildCapabilities(deviceClass)

		// Mask to major+minor class bits only (matches Android's getDeviceClass()).
		deviceTypeCode := deviceClass & 0x1FFC

		// Extract manufacturer ID (first one found, or blank).
		mfgrID := ""
		for _, md := range device.AdvertisementPayload.ManufacturerData() {
			mfgrID = fmt.Sprintf("%d", md.CompanyID)
			break
		}

		row := []string{
			addr,               // MAC / BD_ADDR
			device.LocalName(), // SSID / Device Name
			capabilities,       // AuthMode / Capabilities
			firstSeen[addr].Format("2006-01-02 15:04:05"), // FirstSeen
			"0",                                  // Channel
			fmt.Sprintf("%d", deviceTypeCode),    // Frequency / Device Type code
			fmt.Sprintf("%d", device.RSSI),       // RSSI
			fmt.Sprintf("%f", loc.Latitude),      // Latitude
			fmt.Sprintf("%f", loc.Longitude),     // Longitude
			fmt.Sprintf("%d", int(loc.Altitude)), // Altitude
			fmt.Sprintf("%f", loc.Error),         // Accuracy
			"",                                   // RCOIs (blank)
			mfgrID,                               // MfgrId
			"BLE",                                // Type
		}

		writer.Write(row)
		writer.Flush()

		fmt.Printf("Found device: %s (%s) Class: 0x%06X Capabilities: %s\n",
			addr, device.LocalName(), deviceClass, capabilities)
	})
	if err != nil {
		fmt.Println("failed to start scan:", err)
	}

	for {
	}
}

// getDeviceClass queries BlueZ via D-Bus for the device's Class of Device value.
func getDeviceClass(conn *dbus.Conn, addr string) uint32 {
	sanitized := strings.ReplaceAll(addr, ":", "_")
	path := dbus.ObjectPath("/org/bluez/hci0/dev_" + sanitized)
	obj := conn.Object("org.bluez", path)

	v, err := obj.GetProperty("org.bluez.Device1.Class")
	if err == nil {
		if class, ok := v.Value().(uint32); ok {
			return class
		}
	}
	return 0
}

// buildCapabilities returns a WiGLE-style capabilities string from the
// Bluetooth Class of Device, matching the Android app's DEVICE_TYPE_LEGEND.
// Uses getDeviceClass() equivalent: (class & 0x1FFC) for major+minor lookup.
func buildCapabilities(class uint32) string {
	deviceClass := class & 0x1FFC
	name := deviceTypeLegend(deviceClass)

	// Append [LE] for BLE scan type, matching WiGLE convention.
	if name != "" {
		return name + " [LE]"
	}
	return "[LE]"
}

// deviceTypeLegend mirrors the WiGLE Android app's DEVICE_TYPE_LEGEND map.
// Keys are BluetoothClass.Device constants (major+minor, bits 2–12 of CoD).
func deviceTypeLegend(deviceClass uint32) string {
	switch deviceClass {
	// Misc
	case 0x0000:
		return "Misc"
	// Computer
	case 0x0100:
		return "Computer"
	case 0x0104:
		return "Desktop"
	case 0x0108:
		return "Server"
	case 0x010C:
		return "Laptop"
	case 0x0110:
		return "PDA"
	case 0x0114:
		return "Palm"
	case 0x0118:
		return "Wearable Computer"
	// Phone
	case 0x0200:
		return "Phone"
	case 0x0204:
		return "Cellphone"
	case 0x0208:
		return "Cordless Phone"
	case 0x020C:
		return "Smartphone"
	case 0x0210:
		return "Modem/GW"
	case 0x0214:
		return "ISDN"
	// Audio/Video
	case 0x0400:
		return "A/V"
	case 0x0404:
		return "Headset"
	case 0x0408:
		return "Handsfree"
	case 0x0410:
		return "Mic"
	case 0x0414:
		return "Speaker"
	case 0x0418:
		return "Headphones"
	case 0x041C:
		return "Portable Audio"
	case 0x0420:
		return "Car Audio"
	case 0x0428:
		return "HiFi"
	case 0x0430:
		return "Monitor"
	case 0x0434:
		return "Settop"
	case 0x0438:
		return "Camera"
	case 0x043C:
		return "VCR"
	case 0x0440:
		return "Videoconf"
	case 0x0448:
		return "AV Toy"
	case 0x044C:
		return "Display/Speaker"
	case 0x0456:
		return "Camcorder"
	// Peripheral
	case 0x0500:
		return "Keyboard !p"
	case 0x0540:
		return "Keyboard"
	case 0x0580:
		return "Pointer"
	case 0x05C0:
		return "Keyboard+p"
	// Imaging (0x0600) — not in WiGLE legend
	// Wearable
	case 0x0700:
		return "Wearable"
	case 0x0704:
		return "Watch"
	case 0x0708:
		return "Jacket"
	case 0x070C:
		return "Pager"
	case 0x0710:
		return "Helmet"
	case 0x0714:
		return "Glasses"
	// Toy
	case 0x0800:
		return "Toy"
	case 0x0804:
		return "Robot"
	case 0x0808:
		return "Vehicle"
	case 0x080C:
		return "Doll"
	case 0x0814:
		return "Game"
	case 0x0820:
		return "Controller"
	// Health
	case 0x0900:
		return "Health"
	case 0x0904:
		return "Blood Pressure"
	case 0x0908:
		return "Thermometer"
	case 0x090C:
		return "Scale"
	case 0x0910:
		return "Glucose"
	case 0x0914:
		return "PulseOxy"
	case 0x0918:
		return "Pulse"
	case 0x091C:
		return "Health Display"
	// Uncategorized (Major.UNCATEGORIZED)
	case 0x1F00:
		return "Uncategorized"
	default:
		return "Misc"
	}
}

func must(action string, err error) {
	if err != nil {
		panic("failed to " + action + ": " + err.Error())
	}
}
