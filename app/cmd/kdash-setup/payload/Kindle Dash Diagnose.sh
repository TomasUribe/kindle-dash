#!/bin/sh
# Name: Kindle Dash Diagnose
# Author: Calendar and Spotify
# Collects facts about this Kindle and tests the connections, then saves them to
# /mnt/us/kindle-dash/diag.txt. Plug the Kindle into a computer to read the file.
# It does not stop the Kindle's own screen.
DIR=/mnt/us/kindle-dash
OUT="$DIR/diag.txt"
RUN=/var/tmp/kdash-run
mkdir -p "$RUN"
lipc-set-prop com.lab126.cmd wirelessEnable 1 2>/dev/null
sleep 6
{
    echo "== Kindle Dash diagnose, $(date)"
    echo "user: $(id)"
    uname -a
    echo "-- cpu";     grep -i -E "model name|processor|hardware|features" /proc/cpuinfo | head -8
    echo "-- memory";  head -3 /proc/meminfo
    echo "-- storage"; df -h /mnt/us /var/tmp 2>/dev/null
    echo "-- mounts";  grep -E " /mnt/us | /var/tmp | / " /proc/mounts
    echo "-- tools"
    for t in fbink curl wget od hexdump evtest lipc-get-prop lipc-set-prop lipc-wait-event setsid nohup usleep timeout stop start; do
        printf '%s: ' "$t"; command -v "$t" || echo "(missing)"
    done
    echo "-- fbink locations"
    ls -l /mnt/us/libkh/bin/ /mnt/us/koreader/fbink /var/tmp/fbink 2>&1 | head -10
    echo "-- exec test: can programs be started straight from /mnt/us?"
    ls -l "$DIR/kdash"
    "$DIR/kdash" -h >/dev/null 2>&1; echo "direct run from /mnt/us: exit code $? (126 or 'Permission denied' means no)"
    cp -f "$DIR/kdash" "$RUN/kdash" && chmod 755 "$RUN/kdash" && { "$RUN/kdash" -h >/dev/null 2>&1; echo "run from $RUN: exit code $?"; }
    echo "-- input devices"; cat /proc/bus/input/devices
    echo "-- battery"; lipc-get-prop com.lab126.powerd battLevel 2>&1
    echo "-- front light"
    for prop in flIntensity flMaxIntensity currentAmberLevel flAuto; do
        printf '%s: ' "$prop"; lipc-get-prop com.lab126.powerd "$prop" 2>&1
    done
    lipc-probe -v com.lab126.powerd 2>&1 | grep -i -E "amber|warm|fl[A-Z]|bright" | head -20
    echo "-- wifi";    lipc-get-prop com.lab126.wifid cmState 2>&1; cat /etc/resolv.conf 2>&1
    echo "-- ssl";     ls -l /etc/ssl 2>&1 | head -5
    echo "-- init";    ls /etc/init 2>&1 | grep -i -E "gui|framework|status" ; ls /etc/init.d 2>&1 | grep -i framework
    echo
    export SSL_CERT_FILE="$DIR/cacert.pem"
    FB=""
    for f in /mnt/us/libkh/bin/fbink /mnt/us/koreader/fbink; do [ -f "$f" ] && { cp -f "$f" "$RUN/fbink" && chmod 755 "$RUN/fbink" && FB="$RUN/fbink"; break; }; done
    "$RUN/kdash" -config "$DIR/config.json" ${FB:+-fbink "$FB"} -check 2>&1
} > "$OUT" 2>&1
head -70 "$OUT"
echo
echo "Saved to $OUT"
echo "Plug the Kindle into the computer to read it."
