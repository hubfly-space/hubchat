package realtime

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

func eventID() string {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return "evt_unknown"
	}
	return "evt_" + hex.EncodeToString(buf)
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
