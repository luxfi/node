package math

// Re-export commonly used math functions from github.com/ethereum/go-ethereum/common/math
import (
	"math/big"

	"github.com/ethereum/go-ethereum/common/math"
)

var (
	BigPow = math.BigPow
)

// BigMax returns the larger of x or y.
func BigMax(x, y *big.Int) *big.Int {
	if x.Cmp(y) < 0 {
		return y
	}
	return x
}

// BigMin returns the smaller of x or y.
func BigMin(x, y *big.Int) *big.Int {
	if x.Cmp(y) > 0 {
		return y
	}
	return x
}