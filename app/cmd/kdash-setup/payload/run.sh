#!/bin/sh
# Kindle Dash launcher. Stops the Kindle's own screen, runs kdash, then puts the Kindle back.
#
# If anything ever gets stuck: hold the power button for about 40 seconds to reboot.
# Nothing here starts at boot, so a reboot always brings back the normal Kindle.
#
# Ideas taken from KOReader's launcher on the same Kindle: programs on the storage partition
# (vfat via fuse) may not be executable, so they are copied to /var/tmp first; stopping the
# interface sends SIGTERM, which must not kill us; the library launcher is left to finish first.

DIR=/mnt/us/kindle-dash
RUN=/var/tmp/kdash-run
RUNLOG="$DIR/launcher.log"
log() { echo "$(date '+%Y-%m-%d %H:%M:%S') $*" >> "$RUNLOG"; }

# Detach from the library's script launcher, otherwise it can bring the home screen back over us.
if [ "$1" != "--detached" ]; then
    if command -v setsid >/dev/null 2>&1; then
        setsid /bin/sh "$0" --detached >/dev/null 2>&1 </dev/null &
    else
        nohup /bin/sh "$0" --detached >/dev/null 2>&1 </dev/null &
    fi
    exit 0
fi
exec >>"$RUNLOG" 2>&1
cd / || exit 1
sleep 3  # let the launcher finish and return to the home screen first

mkdir -p "$RUN"
find_fbink() {
    for f in /mnt/us/libkh/bin/fbink /mnt/us/koreader/fbink /var/tmp/fbink /usr/bin/fbink /usr/local/bin/fbink "$(command -v fbink)"; do
        [ -n "$f" ] && [ -f "$f" ] && { echo "$f"; return; }
    done
}
FBINK_SRC=$(find_fbink)
FBINK=""
if [ -n "$FBINK_SRC" ] && cp -f "$FBINK_SRC" "$RUN/fbink" && chmod 755 "$RUN/fbink"; then FBINK="$RUN/fbink"; fi

message() {
    [ -n "$FBINK" ] && "$FBINK" -q -c -f -m -M "$1"
}

gui() {  # gui start|stop
    if command -v "$1" >/dev/null 2>&1; then "$1" lab126_gui
    else /etc/init.d/framework "$1"; fi
}

KPID=""
USBW=""
cleanup() {
    trap '' TERM INT HUP
    [ -n "$USBW" ] && kill "$USBW" 2>/dev/null
    for p in $( (ps -o pid,args 2>/dev/null || ps) | awk '/lipc-wait-event com.lab126.hal/ && !/awk/ { print $1 }'); do
        kill "$p" 2>/dev/null
    done
    [ -n "$KPID" ] && kill "$KPID" 2>/dev/null
    message "Closing Kindle Dash... the home screen takes up to a minute to return"
    gui start
    start statusbar >/dev/null 2>&1   # harmless if it already came back with the interface
    # The interface keeps loading long after it is "started" and will not redraw by itself:
    # ask for the home screen until the app manager accepts.
    i=0
    while [ "$i" -lt 18 ]; do
        sleep 5
        lipc-set-prop com.lab126.appmgrd start app://com.lab126.booklet.home 2>/dev/null && break
        i=$((i + 1))
    done
    lipc-set-prop com.lab126.powerd preventScreenSaver 0 2>/dev/null
    rm -f "$RUN/kdash"
    log "stopped (home screen requested after about $(((i + 1) * 5)) s)"
}
trap 'cleanup; exit 0' TERM INT HUP

log "starting (fbink: ${FBINK_SRC:-none})"
if [ ! -f "$DIR/kdash" ]; then
    log "kdash program is missing"
    message "Kindle Dash is not installed correctly (kdash missing)"
    exit 1
fi
if cp -f "$DIR/kdash" "$RUN/kdash" && chmod 755 "$RUN/kdash"; then
    PROG="$RUN/kdash"
else
    log "could not copy kdash to $RUN; trying it in place"
    PROG="$DIR/kdash"
fi

lipc-set-prop com.lab126.powerd preventScreenSaver 1 2>/dev/null   # do not sleep while it runs
lipc-set-prop com.lab126.cmd wirelessEnable 1 2>/dev/null          # Wi-Fi is needed for data

# Stopping the interface sends SIGTERM to everything it started: ignore it just for this moment.
trap '' TERM
gui stop
if command -v usleep >/dev/null 2>&1; then usleep 1250000; else sleep 2; fi
trap 'cleanup; exit 0' TERM INT HUP
stop statusbar >/dev/null 2>&1   # a separate job on this Kindle; harmless if already stopped

export SSL_CERT_FILE="$DIR/cacert.pem"
START=$(date +%s)
"$PROG" -config "$DIR/config.json" -log "$DIR/kdash.log" ${FBINK:+-fbink "$FBINK"} &
KPID=$!

# Plugging in a USB cable means "transfer files": hand the Kindle back to its own interface.
( lipc-wait-event com.lab126.hal usbPlugIn,usbConfigured >/dev/null 2>&1 && kill -TERM "$KPID" 2>/dev/null ) &
USBW=$!

wait "$KPID"
CODE=$?
log "kdash ended with code $CODE after $(( $(date +%s) - START )) s"
if [ "$CODE" -ne 0 ] && [ $(( $(date +%s) - START )) -lt 10 ]; then
    message "Kindle Dash could not start (see kdash.log). Going back to the home screen."
    sleep 4
fi
KPID=""
cleanup
exit 0
