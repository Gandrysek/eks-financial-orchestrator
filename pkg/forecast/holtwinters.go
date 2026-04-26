// Package forecast implements cost forecasting using Holt-Winters triple
// exponential smoothing. It generates 7, 14, and 30-day forecasts with
// confidence intervals based on 90 days of historical cost data.
package forecast

// holtWintersForecast implements the Holt-Winters additive model with level,
// trend, and seasonal components. It supports weekly (7-day) seasonality.
//
// Parameters:
//   - data: historical daily cost values
//   - seasonLength: number of data points per season (e.g. 7 for weekly)
//   - alpha: level smoothing parameter (0 < alpha < 1)
//   - beta: trend smoothing parameter (0 < beta < 1)
//   - gamma: seasonal smoothing parameter (0 < gamma < 1)
//   - forecastDays: number of days to forecast ahead
//
// Returns a slice of forecasted values for each day in the forecast horizon.
func holtWintersForecast(data []float64, seasonLength int, alpha, beta, gamma float64, forecastDays int) []float64 {
	n := len(data)
	if n < 2*seasonLength {
		// Not enough data for initialization; return zeros.
		result := make([]float64, forecastDays)
		return result
	}

	// --- Initialization ---

	// Level: mean of the first season.
	level := 0.0
	for i := 0; i < seasonLength; i++ {
		level += data[i]
	}
	level /= float64(seasonLength)

	// Trend: average difference between corresponding points in the first two seasons.
	trend := 0.0
	for i := 0; i < seasonLength; i++ {
		trend += (data[seasonLength+i] - data[i])
	}
	trend /= float64(seasonLength * seasonLength)

	// Seasonal factors: deviation of each point in the first season from the initial level.
	seasonal := make([]float64, n+forecastDays)
	for i := 0; i < seasonLength; i++ {
		seasonal[i] = data[i] - level
	}

	// --- Iterative update ---

	for t := seasonLength; t < n; t++ {
		val := data[t]

		prevLevel := level
		// Update level.
		level = alpha*(val-seasonal[t-seasonLength]) + (1-alpha)*(prevLevel+trend)
		// Update trend.
		trend = beta*(level-prevLevel) + (1-beta)*trend
		// Update seasonal component.
		seasonal[t] = gamma*(val-level) + (1-gamma)*seasonal[t-seasonLength]
	}

	// --- Forecast ---

	result := make([]float64, forecastDays)
	for i := 0; i < forecastDays; i++ {
		m := i + 1
		seasonIdx := n - seasonLength + ((m - 1) % seasonLength)
		result[i] = level + float64(m)*trend + seasonal[seasonIdx]
	}

	return result
}
