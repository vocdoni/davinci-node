package txmanager

import "strings"

func isNonceTooHigh(err error) bool {
	return containsErr(err, "nonce too high")
}

func isNonceTooLow(err error) bool {
	return containsErr(err, "nonce too low")
}

func isUnderpriced(err error) bool {
	return containsErr(err, "replacement transaction underpriced") ||
		containsErr(err, "transaction underpriced") ||
		containsErr(err, "tip too low")
}

func isFeeTooLow(err error) bool {
	return containsErr(err, "fee cap too low") ||
		containsErr(err, "max priority fee per gas higher than max fee per gas") ||
		containsErr(err, "max fee per gas less than block base fee")
}

func isAlreadyKnown(err error) bool {
	return containsErr(err, "already known")
}

func isBenignSendErr(err error) bool {
	return isAlreadyKnown(err) || isNonceTooLow(err)
}

func containsErr(err error, sub string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), strings.ToLower(sub))
}
