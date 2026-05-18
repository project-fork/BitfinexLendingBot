package rates

import (
	"github.com/kfrico/BitfinexLendingBot/internal/constants"
)

// Converter 利率转换器
type Converter struct{}

// NewConverter 创建新的利率转换器
func NewConverter() *Converter {
	return &Converter{}
}

// PercentageToDecimal 将百分比转换为小数
func (c *Converter) PercentageToDecimal(percentage float64) float64 {
	return percentage / constants.PercentageToDecimal
}

// DecimalToPercentage 将小数转换为百分比
func (c *Converter) DecimalToPercentage(decimal float64) float64 {
	return decimal * constants.PercentageToDecimal
}

// DailyToAnnual 将日利率转换为年利率
func (c *Converter) DailyToAnnual(dailyRate float64) float64 {
	return dailyRate * constants.DaysPerYear
}

// AnnualToDaily 将年利率转换为日利率
func (c *Converter) AnnualToDaily(annualRate float64) float64 {
	return annualRate / constants.DaysPerYear
}

// PercentageDailyToDecimalDaily 将百分比日利率转换为小数日利率
func (c *Converter) PercentageDailyToDecimalDaily(percentageDaily float64) float64 {
	return c.PercentageToDecimal(percentageDaily)
}

// DecimalDailyToPercentageDaily 将小数日利率转换为百分比日利率
func (c *Converter) DecimalDailyToPercentageDaily(decimalDaily float64) float64 {
	return c.DecimalToPercentage(decimalDaily)
}

// PercentageToAnnualDecimal 将百分比转换为年化小数
// 例如：0.5% -> 0.005 -> 1.825 (年化)
func (c *Converter) PercentageToAnnualDecimal(percentage float64) float64 {
	dailyDecimal := c.PercentageToDecimal(percentage)
	return c.DailyToAnnual(dailyDecimal)
}

// AnnualDecimalToPercentage 将年化小数转换为百分比
func (c *Converter) AnnualDecimalToPercentage(annualDecimal float64) float64 {
	dailyDecimal := c.AnnualToDaily(annualDecimal)
	return c.DecimalToPercentage(dailyDecimal)
}

// ValidateDailyRate 验证日利率是否在合理范围内
func (c *Converter) ValidateDailyRate(dailyRate float64) bool {
	// Bitfinex 限制每日利率不超过 7%
	const maxDailyRateDecimal = 0.07
	return dailyRate > 0 && dailyRate <= maxDailyRateDecimal
}

// ValidatePercentageRate 验证百分比利率是否在合理范围内
func (c *Converter) ValidatePercentageRate(percentageRate float64) bool {
	// 转换为日利率进行验证
	dailyRate := c.PercentageToDecimal(percentageRate)
	return c.ValidateDailyRate(dailyRate)
}
