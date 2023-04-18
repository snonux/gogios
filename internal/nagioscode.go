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
	case ok:
		return "OK"
	case warning:
		return "WARNING"
	case critical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}
