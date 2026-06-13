package orders

const (
	StatusPlaced         = "placed"
	StatusConfirmed      = "confirmed"
	StatusPreparing      = "preparing"
	StatusReady          = "ready"
	StatusOutForDelivery = "out_for_delivery"
	StatusDelivered      = "delivered"
	StatusCancelled      = "cancelled"
	StatusFailed         = "failed"
)

func CanTransition(from, to string) bool {
	switch from {
	case StatusPlaced:
		return to == StatusConfirmed || to == StatusCancelled || to == StatusFailed
	case StatusConfirmed:
		return to == StatusPreparing || to == StatusCancelled || to == StatusFailed
	case StatusPreparing:
		return to == StatusReady || to == StatusCancelled || to == StatusFailed
	case StatusReady:
		return to == StatusOutForDelivery || to == StatusCancelled || to == StatusFailed
	case StatusOutForDelivery:
		return to == StatusDelivered || to == StatusFailed
	default:
		return false
	}
}

func NextStatus(current string) (string, bool) {
	switch current {
	case StatusPlaced:
		return StatusConfirmed, true
	case StatusConfirmed:
		return StatusPreparing, true
	case StatusPreparing:
		return StatusReady, true
	case StatusReady:
		return StatusOutForDelivery, true
	case StatusOutForDelivery:
		return StatusDelivered, true
	default:
		return "", false
	}
}

func IsTerminal(status string) bool {
	return status == StatusDelivered || status == StatusCancelled || status == StatusFailed
}
