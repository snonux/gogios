package internal

type nagiosCode int

const (
	ok       nagiosCode = 0
	warning  nagiosCode = 1
	critical nagiosCode = 2
	unknown  nagiosCode = 3
)

func (n nagiosCode) Str() string {
	switch n {
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
