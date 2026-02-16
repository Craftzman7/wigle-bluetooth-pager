#!/bin/bash
# Title: wigle-bluetooth
# Description: Log BLE devices to a Wigle-compatible CSV file (requires GPS).
# Author: Evelyn (@evecontextprotocol on Discord)
# Version: 1.0
# Category: Reconnaissance

if [ ! -f "wiglebluetooth" ]; then
    echo "Error: wiglebluetooth executable not found. Please compile it first."
    exit 1
fi

chmod +x wiglebluetooth

while true; do
    LOG ""
    LOG green "Press UP to start the wigle-bluetooth process"
    LOG red "Press DOWN to stop the wigle-bluetooth process"
    LOG blue "Press right to combine the latest wigle-bluetooth CSV with the Pineapple's latest CSV."
    LOG yellow "Press LEFT to exit the script"
    LOG ""

    choice=$(WAIT_FOR_INPUT)

    if [ "$choice" == "UP" ]; then
        if pgrep wiglebluetooth > /dev/null; then
            LOG yellow "wigle-bluetooth is already running."
        else
            LOG green "Starting wigle-bluetooth..."
            ./wiglebluetooth &
            LOG green "wigle-bluetooth started. Logging in /root/loot/wigle-bluetooth"
        fi
    elif [ "$choice" == "DOWN" ]; then
        if pgrep wiglebluetooth > /dev/null; then
            LOG red "Stopping wigle-bluetooth..."
            kilall wiglebluetooth
            LOG red "wigle-bluetooth stopped."
        else
            LOG yellow "wigle-bluetooth is not running."
        fi
    elif [ "$choice" == "RIGHT" ]; then
        # Use the latest CSV file in /root/loot/wigle-bluetooth and the latest CSV file in /root/loot/wigle
        latest_bluetooth_csv=$(ls -t /root/loot/wigle-bluetooth/*.csv 2>/dev/null | head -n 1)
        latest_wifi_csv=$(ls -t /root/loot/wigle/*.csv 2>/dev/null | head -n 1)
        if [ -z "$latest_bluetooth_csv" ]; then
            LOG yellow "No Bluetooth CSV file found in /root/loot/wigle-bluetooth."
            continue
        fi
        if [ -z "$latest_wifi_csv" ]; then
            LOG yellow "No Wi-Fi CSV file found in /root/loot/wigle."
            continue
        fi
        combined_csv="/root/loot/wigle/wigle-combined_$(date +%Y%m%d_%H%M%S).csv"
        mkdir -p /root/loot/wigle
        echo "MAC,SSID,AuthMode,FirstSeen,Channel,Latitude,Longitude,Altitude" > "$combined_csv"
        tail -n +2 "$latest_bluetooth_csv" >> "$combined_csv"
        tail -n +2 "$latest_wifi_csv" >> "$combined_csv"
        LOG green "Combined CSV created at $combined_csv"
    elif [ "$choice" == "LEFT" ]; then
        LOG yellow "Exiting the script..."
        exit 0
    else
        LOG red "Invalid input. Please try again."
    fi
done