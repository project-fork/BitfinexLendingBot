package strategy

import "math"

func floorToCents(amount float64) float64 {
	return math.Floor((amount+1e-9)*100) / 100
}

// buildOrderAmounts 尽量平均分配全部资金，并确保每笔金额介于 MIN_LOAN 与 MAX_LOAN 之间。
func buildOrderAmounts(totalFunds float64, requestedSplits int, minLoan float64, maxLoan float64) []float64 {
	if requestedSplits <= 0 || totalFunds < minLoan {
		return nil
	}

	orderCount := requestedSplits

	avgAmount := totalFunds / float64(orderCount)

	// 单笔不足最小金额时，减少笔数直到可行。
	for avgAmount < minLoan && orderCount > 1 {
		orderCount--
		avgAmount = totalFunds / float64(orderCount)
	}

	if avgAmount < minLoan {
		return nil
	}

	// 单笔超过最大金额时，维持原笔数并将每笔封顶，剩余金额保留不下。
	if maxLoan > 0 && avgAmount > maxLoan {
		amounts := make([]float64, 0, orderCount)
		for i := 0; i < orderCount; i++ {
			amounts = append(amounts, maxLoan)
		}
		return amounts
	}

	totalCents := int(math.Floor((totalFunds + 1e-9) * 100))
	baseCents := totalCents / orderCount
	remainderCents := totalCents % orderCount

	amounts := make([]float64, 0, orderCount)
	for i := 0; i < orderCount; i++ {
		cents := baseCents
		if i < remainderCents {
			cents++
		}

		amount := float64(cents) / 100
		if amount < minLoan {
			return nil
		}
		if maxLoan > 0 && amount > maxLoan {
			return nil
		}
		amounts = append(amounts, amount)
	}

	return amounts
}
