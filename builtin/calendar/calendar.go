// Package calendar 提供中国法定节假日数据和工作日计算工具。
//
// 主要功能：
//   - 判断指定日期是否为法定节假日或工作日
//   - 计算距离下一个假期的天数
//   - 获取下一个工作日
//   - 提供节假日列表查询
//
// 节假日数据覆盖 2025-2027 年，参考国务院公告。
//
// 使用示例：
//
//	today := time.Now()
//	if calendar.IsHoliday(today) {
//	    fmt.Println("今天是假期！")
//	}
//	next, days := calendar.NextHoliday(today)
//	fmt.Printf("距离 %s 还有 %d 天", next.Name, days)
package calendar

import (
	"time"
)

// Holiday 节假日信息
type Holiday struct {
	// Date 节假日日期（年-月-日）
	Date string
	// Name 节假日名称（如"春节"、"国庆节"）
	Name string
	// IsWorkday 是否为补班工作日（调休）
	IsWorkday bool
}

// holidays2025 2025年中国法定节假日（参考国务院公告）
var holidays2025 = []Holiday{
	// 元旦
	{"2025-01-01", "元旦", false},
	// 春节
	{"2025-01-26", "春节", false},
	{"2025-01-27", "春节", false},
	{"2025-01-28", "春节", false},
	{"2025-01-29", "春节", false},
	{"2025-01-30", "春节", false},
	{"2025-01-31", "春节", false},
	{"2025-02-01", "春节", false},
	{"2025-02-02", "春节", false},
	// 清明节
	{"2025-04-04", "清明节", false},
	{"2025-04-05", "清明节", false},
	{"2025-04-06", "清明节", false},
	// 劳动节
	{"2025-05-01", "劳动节", false},
	{"2025-05-02", "劳动节", false},
	{"2025-05-03", "劳动节", false},
	{"2025-05-04", "劳动节", false},
	{"2025-05-05", "劳动节", false},
	// 端午节
	{"2025-05-31", "端午节", false},
	{"2025-06-01", "端午节", false},
	{"2025-06-02", "端午节", false},
	// 国庆节/中秋节
	{"2025-10-01", "国庆节/中秋节", false},
	{"2025-10-02", "国庆节/中秋节", false},
	{"2025-10-03", "国庆节/中秋节", false},
	{"2025-10-04", "国庆节/中秋节", false},
	{"2025-10-05", "国庆节/中秋节", false},
	{"2025-10-06", "国庆节/中秋节", false},
	{"2025-10-07", "国庆节/中秋节", false},
	{"2025-10-08", "国庆节/中秋节", false},
	// 补班工作日
	{"2025-01-18", "补班（春节）", true},
	{"2025-01-26", "补班（春节）", true},
	{"2025-04-27", "补班（劳动节）", true},
	{"2025-09-28", "补班（国庆节）", true},
	{"2025-10-11", "补班（国庆节）", true},
}

// holidays2026 2026年中国法定节假日（预估，参考往年规律）
var holidays2026 = []Holiday{
	// 元旦
	{"2026-01-01", "元旦", false},
	{"2026-01-02", "元旦", false},
	{"2026-01-03", "元旦", false},
	// 春节（2026年春节：1月17日除夕）
	{"2026-01-17", "春节", false},
	{"2026-01-18", "春节", false},
	{"2026-01-19", "春节", false},
	{"2026-01-20", "春节", false},
	{"2026-01-21", "春节", false},
	{"2026-01-22", "春节", false},
	{"2026-01-23", "春节", false},
	// 清明节
	{"2026-04-04", "清明节", false},
	{"2026-04-05", "清明节", false},
	{"2026-04-06", "清明节", false},
	// 劳动节
	{"2026-05-01", "劳动节", false},
	{"2026-05-02", "劳动节", false},
	{"2026-05-03", "劳动节", false},
	{"2026-05-04", "劳动节", false},
	{"2026-05-05", "劳动节", false},
	// 端午节（2026年6月19日）
	{"2026-06-19", "端午节", false},
	{"2026-06-20", "端午节", false},
	{"2026-06-21", "端午节", false},
	// 中秋节（2026年9月25日）
	{"2026-09-25", "中秋节", false},
	{"2026-09-26", "中秋节", false},
	{"2026-09-27", "中秋节", false},
	// 国庆节
	{"2026-10-01", "国庆节", false},
	{"2026-10-02", "国庆节", false},
	{"2026-10-03", "国庆节", false},
	{"2026-10-04", "国庆节", false},
	{"2026-10-05", "国庆节", false},
	{"2026-10-06", "国庆节", false},
	{"2026-10-07", "国庆节", false},
}

// holidays2027 2027年中国法定节假日（预估）
var holidays2027 = []Holiday{
	// 元旦
	{"2027-01-01", "元旦", false},
	{"2027-01-02", "元旦", false},
	{"2027-01-03", "元旦", false},
	// 春节（2027年农历正月初一：2月6日）
	{"2027-02-06", "春节", false},
	{"2027-02-07", "春节", false},
	{"2027-02-08", "春节", false},
	{"2027-02-09", "春节", false},
	{"2027-02-10", "春节", false},
	{"2027-02-11", "春节", false},
	{"2027-02-12", "春节", false},
	// 清明节
	{"2027-04-05", "清明节", false},
	{"2027-04-06", "清明节", false},
	{"2027-04-07", "清明节", false},
	// 劳动节
	{"2027-05-01", "劳动节", false},
	{"2027-05-02", "劳动节", false},
	{"2027-05-03", "劳动节", false},
	{"2027-05-04", "劳动节", false},
	{"2027-05-05", "劳动节", false},
	// 端午节
	{"2027-06-09", "端午节", false},
	{"2027-06-10", "端午节", false},
	{"2027-06-11", "端午节", false},
	// 中秋节
	{"2027-09-15", "中秋节", false},
	{"2027-09-16", "中秋节", false},
	{"2027-09-17", "中秋节", false},
	// 国庆节
	{"2027-10-01", "国庆节", false},
	{"2027-10-02", "国庆节", false},
	{"2027-10-03", "国庆节", false},
	{"2027-10-04", "国庆节", false},
	{"2027-10-05", "国庆节", false},
	{"2027-10-06", "国庆节", false},
	{"2027-10-07", "国庆节", false},
}

// allHolidays 所有已知节假日（合并各年数据）
var allHolidays []Holiday

func init() {
	allHolidays = append(allHolidays, holidays2025...)
	allHolidays = append(allHolidays, holidays2026...)
	allHolidays = append(allHolidays, holidays2027...)
}

// dateStr 将 time.Time 格式化为 "2006-01-02"
func dateStr(t time.Time) string {
	return t.Format("2006-01-02")
}

// findHoliday 在节假日列表中查找指定日期
func findHoliday(dateS string) *Holiday {
	for i := range allHolidays {
		if allHolidays[i].Date == dateS {
			return &allHolidays[i]
		}
	}
	return nil
}

// IsHoliday 判断指定日期是否为法定假期（不含普通周末）。
//
// 返回 true 仅当该日期出现在法定节假日列表中（非调休补班日）。
//
// 注意：如果是普通周末但不在节假日列表内，返回 false；
// 请使用 [IsDayOff] 判断"是否可以休息"（周末 + 法定假日）。
func IsHoliday(t time.Time) bool {
	h := findHoliday(dateStr(t))
	return h != nil && !h.IsWorkday
}

// IsLegalWorkday 判断指定日期是否为法定调休补班工作日。
//
// 某些法定假日前后会有补班安排，这些日期虽然是周末，但属于工作日。
func IsLegalWorkday(t time.Time) bool {
	h := findHoliday(dateStr(t))
	return h != nil && h.IsWorkday
}

// IsDayOff 判断指定日期是否可以休息（法定假日 + 普通周末且非调休）。
//
// 等价于：IsHoliday(t) || (IsWeekend(t) && !IsLegalWorkday(t))
func IsDayOff(t time.Time) bool {
	if IsHoliday(t) {
		return true
	}
	wd := t.Weekday()
	isWeekend := wd == time.Saturday || wd == time.Sunday
	return isWeekend && !IsLegalWorkday(t)
}

// IsWeekend 判断指定日期是否为自然周末（周六或周日），不考虑调休。
func IsWeekend(t time.Time) bool {
	wd := t.Weekday()
	return wd == time.Saturday || wd == time.Sunday
}

// IsWorkday 判断指定日期是否为工作日。
//
// 工作日 = 非法定假期且非普通周末，或者是调休补班日。
func IsWorkday(t time.Time) bool {
	return !IsDayOff(t)
}

// HolidayNameOf 返回指定日期的法定节假日名称。
//
// 若该日期不是法定节假日，返回空字符串。
func HolidayNameOf(t time.Time) string {
	h := findHoliday(dateStr(t))
	if h != nil && !h.IsWorkday {
		return h.Name
	}
	return ""
}

// NextHoliday 查找指定日期之后（含当天）的下一个法定节假日。
//
// 返回节假日信息和距离天数（0 表示今天就是节假日）。
// 若数据库中无后续节假日记录，返回 nil 和 -1。
func NextHoliday(t time.Time) (*Holiday, int) {
	today := dateStr(t)
	todayTime := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())

	seenNames := make(map[string]bool) // 同名节假日只显示一次（取首日）
	for i := range allHolidays {
		h := &allHolidays[i]
		if h.IsWorkday {
			continue
		}
		if h.Date < today {
			continue
		}
		// 同名节假日（连续多天）只显示一次
		if seenNames[h.Name] {
			continue
		}
		seenNames[h.Name] = true
		hTime, err := time.ParseInLocation("2006-01-02", h.Date, t.Location())
		if err != nil {
			continue
		}
		days := max(int(hTime.Sub(todayTime).Hours()/24), 0)
		return h, days
	}
	return nil, -1
}

// DaysUntilWeekend 计算距离最近周末（周六或周日）的天数。
//
// 0 表示今天就是周末；不考虑调休。
func DaysUntilWeekend(t time.Time) int {
	wd := t.Weekday()
	switch wd {
	case time.Saturday, time.Sunday:
		return 0
	case time.Friday:
		return 1
	case time.Thursday:
		return 2
	case time.Wednesday:
		return 3
	case time.Tuesday:
		return 4
	case time.Monday:
		return 5
	}
	return 6
}

// DaysUntilDayOff 计算距离下一个"可以休息的日子"的天数。
//
// 综合考虑法定节假日和周末（并排除调休补班日）。
// 0 表示今天就可以休息。
func DaysUntilDayOff(t time.Time) int {
	cur := t
	for i := 0; i <= 365; i++ {
		if IsDayOff(cur) {
			return i
		}
		cur = cur.AddDate(0, 0, 1)
	}
	return -1
}

// WeekdayName 返回中文星期名称（如"周一"、"周末"）。
func WeekdayName(wd time.Weekday) string {
	names := [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	if int(wd) < len(names) {
		return names[wd]
	}
	return wd.String()
}

// GetHolidaysByYear 返回指定年份的全部节假日列表（含调休）。
//
// 若该年份暂无数据，返回 nil。
func GetHolidaysByYear(year int) []Holiday {
	var result []Holiday
	prefix := time.Date(year, 1, 1, 0, 0, 0, 0, time.Local).Format("2006")
	for _, h := range allHolidays {
		if len(h.Date) >= 4 && h.Date[:4] == prefix {
			result = append(result, h)
		}
	}
	return result
}

// GetMonthHolidays 返回指定年月的节假日列表。
func GetMonthHolidays(year, month int) []Holiday {
	prefix := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.Local).Format("2006-01")
	var result []Holiday
	for _, h := range allHolidays {
		if len(h.Date) >= 7 && h.Date[:7] == prefix {
			result = append(result, h)
		}
	}
	return result
}
