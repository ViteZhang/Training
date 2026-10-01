package rules

import "time"

// Day 是北京时间的一个自然日，用距 1970-01-01 的天数表示，比较与加减都是整数运算。
type Day int32

var shanghai = time.FixedZone("Asia/Shanghai", 8*3600)

// DayOf 返回某个时刻在北京时间的日期。
func DayOf(t time.Time) Day {
	y, m, d := t.In(shanghai).Date()
	return DayFromDate(y, m, d)
}

// DayFromDate 由年月日构造 Day。
func DayFromDate(y int, m time.Month, d int) Day {
	return Day(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

// Time 返回这一天北京时间 0 点对应的时刻。
func (d Day) Time() time.Time {
	return time.Unix(int64(d)*86400, 0).UTC().Add(-8 * time.Hour)
}

// AddDays 返回 n 天后的日期。
func (d Day) AddDays(n int) Day { return d + Day(n) }

// String 返回 YYYY-MM-DD。
func (d Day) String() string {
	return time.Unix(int64(d)*86400, 0).UTC().Format(time.DateOnly)
}
