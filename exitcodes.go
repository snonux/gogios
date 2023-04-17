package main

const (
	ok       = 0
	warning  = 1
	critical = 2
	unknown  = 3
)

func codeToString(state int) string {
	switch state {
	case 0:
		return "OK"
	case 1:
		return "WARNING"
	case 2:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}
