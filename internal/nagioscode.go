package internal

type nagiosCode int

const (
	nagiosOk       nagiosCode = 0
	nagiosWarning  nagiosCode = 1
	nagiosCritical nagiosCode = 2
	nagiosUnknown  nagiosCode = 3
)

func (n nagiosCode) Str() string {
	switch n {
	case nagiosOk:
		return "OK"
	case nagiosWarning:
		return "WARNING"
	case nagiosCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}
