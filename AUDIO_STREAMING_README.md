# Audio Streaming Feature

## Overview
- Bidirectional audio (mic ↔ speaker)
- Echo cancellation (AEC) prevents feedback
- WebRTC-based streaming

⚠️ **Important:** Run JETSON RMCS in your user session (not with sudo). Using sudo causes session conflicts and audio streaming will not work.

---

## Installation

### Install Dependencies
```bash
sudo apt install -y pulseaudio pulseaudio-utils alsa-utils libopus-dev libopusfile-dev pkg-config
```

### Start PulseAudio (Only start one time to make sure that pulseaudio work)
```bash
pulseaudio --start
```

### Test Audio Devices
```bash
# Record 3 seconds (speak loudly)
arecord -d 3 -r 48000 -c 2 -f S16_LE test.wav

# Play it back
aplay test.wav && rm test.wav
```

---

### 🎯 If you got here, let's start JETSON RMCS. No need to repeat the Installation steps next time.
---

## Troubleshooting

### Audio Not Working (Session Issues)
```bash
# ⚠️ DO NOT run RMCS with sudo
# Run in your user session:
./run-jetson.sh <args>

# If you used sudo, stop it and restart without sudo
```

### No Audio
```bash
# Restart PulseAudio
pulseaudio -k && pulseaudio --start

# Check devices
arecord -l && aplay -l
pactl list short sources
pactl list short sinks
```

### Echo/Feedback
```bash
# Verify AEC loaded
pactl list modules | grep echo-cancel

# Reduce mic volume if too loud
pactl set-source-volume @DEFAULT_SOURCE@ 30%
```

### Silent Mic
```bash
# Test mic directly
arecord -d 3 -r 48000 -c 2 -f S16_LE test.wav
aplay test.wav
# If silent, check mic hardware/connections
```

### AEC Not Working

**Check if AEC module is loaded (only works after RMCS has started):**
```bash
pactl list modules short | grep echo-cancel
pactl list short sources | grep echocancel
pactl list short sinks | grep echocancel
```
---

## Quick Commands

```bash
# Check audio status
pactl get-default-source  # Mic
pactl get-default-sink     # Speaker

# View logs
tail -f logs/rmcs.log | grep -E "Audio|AEC"

# Restart audio
pulseaudio -k && pulseaudio --start
```

---

**Audio streaming works automatically when RMCS starts. No additional configuration needed.**
