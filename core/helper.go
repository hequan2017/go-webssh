package core

import "time"

func writeDeadline() time.Time {
	return time.Now().Add(2 * time.Second)
}
