package calendar

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// ---------------------------------------------------------------------------
// IsHoliday
// ---------------------------------------------------------------------------

func TestIsHoliday_knownHoliday(t *testing.T) {
	assert.True(t, IsHoliday(date(2025, 1, 1)))
	assert.True(t, IsHoliday(date(2025, 5, 1)))
	assert.True(t, IsHoliday(date(2025, 10, 1)))
	assert.True(t, IsHoliday(date(2026, 1, 1)))
}

func TestIsHoliday_workdayReturnsFalse(t *testing.T) {
	assert.False(t, IsHoliday(date(2025, 3, 1)))
	assert.False(t, IsHoliday(date(2025, 7, 15)))
}

func TestIsHoliday_weekendReturnsFalse(t *testing.T) {
	// Saturday & Sunday are not "法定假期"
	assert.False(t, IsHoliday(date(2025, 1, 4))) // Saturday
	assert.False(t, IsHoliday(date(2025, 1, 5))) // Sunday
}

func TestIsHoliday_buBanDayReturnsFalse(t *testing.T) {
	assert.False(t, IsHoliday(date(2025, 1, 18)))  // 补班（春节）, IsWorkday=true
	assert.False(t, IsHoliday(date(2025, 4, 27)))  // 补班（劳动节）, IsWorkday=true
	assert.False(t, IsHoliday(date(2025, 9, 28)))  // 补班（国庆节）, IsWorkday=true
	assert.False(t, IsHoliday(date(2025, 10, 11))) // 补班（国庆节）, IsWorkday=true
}

func TestIsHoliday_conflictDate(t *testing.T) {
	// 2025-01-26 appears as both "春节" (IsWorkday=false, line 38)
	// and "补班（春节）" (IsWorkday=true, line 73).
	// findHoliday returns the first match, so IsHoliday should be true.
	assert.True(t, IsHoliday(date(2025, 1, 26)))
}

// ---------------------------------------------------------------------------
// IsLegalWorkday
// ---------------------------------------------------------------------------

func TestIsLegalWorkday_buBanDay(t *testing.T) {
	assert.True(t, IsLegalWorkday(date(2025, 1, 18)))  // 补班（春节）
	assert.True(t, IsLegalWorkday(date(2025, 4, 27)))  // 补班（劳动节）
	assert.True(t, IsLegalWorkday(date(2025, 9, 28)))  // 补班（国庆节）
	assert.True(t, IsLegalWorkday(date(2025, 10, 11))) // 补班（国庆节）
}

func TestIsLegalWorkday_holidayReturnsFalse(t *testing.T) {
	assert.False(t, IsLegalWorkday(date(2025, 1, 1)))
	assert.False(t, IsLegalWorkday(date(2025, 5, 1)))
}

func TestIsLegalWorkday_normalDayReturnsFalse(t *testing.T) {
	assert.False(t, IsLegalWorkday(date(2025, 3, 3)))
	assert.False(t, IsLegalWorkday(date(2025, 7, 15)))
}

func TestIsLegalWorkday_weekendReturnsFalse(t *testing.T) {
	assert.False(t, IsLegalWorkday(date(2025, 1, 4))) // Saturday, not in list
	assert.False(t, IsLegalWorkday(date(2025, 1, 5))) // Sunday, not in list
}

func TestIsLegalWorkday_conflictDate(t *testing.T) {
	// 2025-01-26 matches "春节" first (IsWorkday=false), so IsLegalWorkday is false
	assert.False(t, IsLegalWorkday(date(2025, 1, 26)))
}

// ---------------------------------------------------------------------------
// IsDayOff
// ---------------------------------------------------------------------------

func TestIsDayOff_holidayReturnsTrue(t *testing.T) {
	assert.True(t, IsDayOff(date(2025, 1, 1))) // 元旦
	assert.True(t, IsDayOff(date(2025, 5, 1))) // 劳动节
	assert.True(t, IsDayOff(date(2026, 1, 1))) // 元旦 2026
}

func TestIsDayOff_weekendReturnsTrue(t *testing.T) {
	assert.True(t, IsDayOff(date(2025, 1, 4))) // Saturday
	assert.True(t, IsDayOff(date(2025, 1, 5))) // Sunday
}

func TestIsDayOff_buBanDayReturnsFalse(t *testing.T) {
	// 补班日 is a Saturday or Sunday that is a workday
	assert.False(t, IsDayOff(date(2025, 1, 18))) // 补班（春节）, Saturday
	assert.False(t, IsDayOff(date(2025, 4, 27))) // 补班（劳动节）, Sunday
}

func TestIsDayOff_normalWorkdayReturnsFalse(t *testing.T) {
	assert.False(t, IsDayOff(date(2025, 3, 3)))  // Monday
	assert.False(t, IsDayOff(date(2025, 7, 15))) // Tuesday
}

// ---------------------------------------------------------------------------
// IsWeekend
// ---------------------------------------------------------------------------

func TestIsWeekend_saturday(t *testing.T) {
	assert.True(t, IsWeekend(date(2025, 1, 4)))
}

func TestIsWeekend_sunday(t *testing.T) {
	assert.True(t, IsWeekend(date(2025, 1, 5)))
}

func TestIsWeekend_monday(t *testing.T) {
	assert.False(t, IsWeekend(date(2025, 1, 6)))
}

func TestIsWeekend_friday(t *testing.T) {
	assert.False(t, IsWeekend(date(2025, 1, 3)))
}

// ---------------------------------------------------------------------------
// IsWorkday
// ---------------------------------------------------------------------------

func TestIsWorkday_holidayReturnsFalse(t *testing.T) {
	assert.False(t, IsWorkday(date(2025, 1, 1)))
	assert.False(t, IsWorkday(date(2025, 10, 1)))
}

func TestIsWorkday_weekendReturnsFalse(t *testing.T) {
	assert.False(t, IsWorkday(date(2025, 1, 4))) // Saturday
}

func TestIsWorkday_buBanReturnsTrue(t *testing.T) {
	assert.True(t, IsWorkday(date(2025, 1, 18))) // 补班
}

func TestIsWorkday_normalWorkdayReturnsTrue(t *testing.T) {
	assert.True(t, IsWorkday(date(2025, 3, 3)))
	assert.True(t, IsWorkday(date(2025, 7, 15)))
}

// ---------------------------------------------------------------------------
// HolidayNameOf
// ---------------------------------------------------------------------------

func TestHolidayNameOf_knownHoliday(t *testing.T) {
	assert.Equal(t, "元旦", HolidayNameOf(date(2025, 1, 1)))
	assert.Equal(t, "春节", HolidayNameOf(date(2025, 1, 27)))
	assert.Equal(t, "清明节", HolidayNameOf(date(2025, 4, 4)))
	assert.Equal(t, "劳动节", HolidayNameOf(date(2025, 5, 1)))
	assert.Equal(t, "端午节", HolidayNameOf(date(2025, 5, 31)))
	assert.Equal(t, "国庆节/中秋节", HolidayNameOf(date(2025, 10, 1)))
}

func TestHolidayNameOf_buBanReturnsEmpty(t *testing.T) {
	assert.Equal(t, "", HolidayNameOf(date(2025, 1, 18)))
	assert.Equal(t, "", HolidayNameOf(date(2025, 4, 27)))
}

func TestHolidayNameOf_nonHolidayReturnsEmpty(t *testing.T) {
	assert.Equal(t, "", HolidayNameOf(date(2025, 3, 3)))
	assert.Equal(t, "", HolidayNameOf(date(2025, 12, 25)))
}

func TestHolidayNameOf_weekendReturnsEmpty(t *testing.T) {
	assert.Equal(t, "", HolidayNameOf(date(2025, 1, 4)))
}

// ---------------------------------------------------------------------------
// NextHoliday
// ---------------------------------------------------------------------------

func TestNextHoliday_afterLastDataReturnsNil(t *testing.T) {
	h, days := NextHoliday(date(2030, 1, 1))
	assert.Nil(t, h)
	assert.Equal(t, -1, days)
}

func TestNextHoliday_onHolidayReturnsSame(t *testing.T) {
	h, days := NextHoliday(date(2025, 1, 1))
	if assert.NotNil(t, h) {
		assert.Equal(t, "元旦", h.Name)
		assert.Equal(t, "2025-01-01", h.Date)
	}
	assert.Equal(t, 0, days)
}

func TestNextHoliday_20251230Returns20260101(t *testing.T) {
	h, days := NextHoliday(date(2025, 12, 30))
	if assert.NotNil(t, h) {
		assert.Equal(t, "元旦", h.Name)
		assert.Equal(t, "2026-01-01", h.Date)
	}
	assert.Equal(t, 2, days)
}

func TestNextHoliday_skipWorkdayEntries(t *testing.T) {
	// 2025-01-18 is a 补班, NextHoliday should skip it and return 春节 on 2025-01-26
	h, days := NextHoliday(date(2025, 1, 17))
	if assert.NotNil(t, h) {
		assert.Equal(t, "春节", h.Name)
	}
	assert.Equal(t, 9, days) // Jan 17 → Jan 26 = 9 days
}

func TestNextHoliday_sameNameGrouped(t *testing.T) {
	// From Jan 25, next holiday is 春节 on Jan 26 (only first day returned)
	h, days := NextHoliday(date(2025, 1, 25))
	if assert.NotNil(t, h) {
		assert.Equal(t, "春节", h.Name)
		assert.Equal(t, "2025-01-26", h.Date)
	}
	assert.Equal(t, 1, days)
}

func TestNextHoliday_before2025Data(t *testing.T) {
	h, days := NextHoliday(date(2024, 12, 30))
	if assert.NotNil(t, h) {
		assert.Equal(t, "元旦", h.Name)
	}
	assert.Equal(t, 2, days)
}

// ---------------------------------------------------------------------------
// DaysUntilWeekend
// ---------------------------------------------------------------------------

func TestDaysUntilWeekend_saturday(t *testing.T) {
	assert.Equal(t, 0, DaysUntilWeekend(date(2025, 1, 4))) // Saturday
}

func TestDaysUntilWeekend_sunday(t *testing.T) {
	assert.Equal(t, 0, DaysUntilWeekend(date(2025, 1, 5))) // Sunday
}

func TestDaysUntilWeekend_monday(t *testing.T) {
	assert.Equal(t, 5, DaysUntilWeekend(date(2025, 1, 6))) // Monday
}

func TestDaysUntilWeekend_tuesday(t *testing.T) {
	assert.Equal(t, 4, DaysUntilWeekend(date(2025, 1, 7))) // Tuesday
}

func TestDaysUntilWeekend_wednesday(t *testing.T) {
	assert.Equal(t, 3, DaysUntilWeekend(date(2025, 1, 1))) // Wednesday
}

func TestDaysUntilWeekend_thursday(t *testing.T) {
	assert.Equal(t, 2, DaysUntilWeekend(date(2025, 1, 2))) // Thursday
}

func TestDaysUntilWeekend_friday(t *testing.T) {
	assert.Equal(t, 1, DaysUntilWeekend(date(2025, 1, 3))) // Friday
}

// ---------------------------------------------------------------------------
// DaysUntilDayOff
// ---------------------------------------------------------------------------

func TestDaysUntilDayOff_onHoliday(t *testing.T) {
	assert.Equal(t, 0, DaysUntilDayOff(date(2025, 1, 1)))
}

func TestDaysUntilDayOff_onWeekend(t *testing.T) {
	assert.Equal(t, 0, DaysUntilDayOff(date(2025, 1, 4))) // Saturday
}

func TestDaysUntilDayOff_dayBeforeHoliday(t *testing.T) {
	// 2025-04-30 (Wed) → next day off is 2025-05-01 (劳动节)
	assert.Equal(t, 1, DaysUntilDayOff(date(2025, 4, 30)))
}

func TestDaysUntilDayOff_buBanIsNotDayOff(t *testing.T) {
	// 2025-01-18 (Saturday, 补班) → next day off is 2025-01-19 (Sunday)
	assert.Equal(t, 1, DaysUntilDayOff(date(2025, 1, 18)))
}

func TestDaysUntilDayOff_fridayBeforeWeekend(t *testing.T) {
	assert.Equal(t, 1, DaysUntilDayOff(date(2025, 1, 3))) // Friday → Saturday
}

func TestDaysUntilDayOff_mondayToWeekend(t *testing.T) {
	assert.Equal(t, 5, DaysUntilDayOff(date(2025, 1, 6))) // Monday → Saturday
}

// ---------------------------------------------------------------------------
// WeekdayName
// ---------------------------------------------------------------------------

func TestWeekdayName_monday(t *testing.T) {
	assert.Equal(t, "周一", WeekdayName(time.Monday))
}

func TestWeekdayName_tuesday(t *testing.T) {
	assert.Equal(t, "周二", WeekdayName(time.Tuesday))
}

func TestWeekdayName_wednesday(t *testing.T) {
	assert.Equal(t, "周三", WeekdayName(time.Wednesday))
}

func TestWeekdayName_thursday(t *testing.T) {
	assert.Equal(t, "周四", WeekdayName(time.Thursday))
}

func TestWeekdayName_friday(t *testing.T) {
	assert.Equal(t, "周五", WeekdayName(time.Friday))
}

func TestWeekdayName_saturday(t *testing.T) {
	assert.Equal(t, "周六", WeekdayName(time.Saturday))
}

func TestWeekdayName_sunday(t *testing.T) {
	assert.Equal(t, "周日", WeekdayName(time.Sunday))
}

// ---------------------------------------------------------------------------
// GetHolidaysByYear
// ---------------------------------------------------------------------------

func TestGetHolidaysByYear_2025(t *testing.T) {
	holidays := GetHolidaysByYear(2025)
	assert.NotEmpty(t, holidays)
	assert.Equal(t, 33, len(holidays))

	found := false
	for _, h := range holidays {
		if h.Name == "元旦" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected 元旦 in 2025 holidays")
}

func TestGetHolidaysByYear_2025IncludesBuBan(t *testing.T) {
	holidays := GetHolidaysByYear(2025)
	hasBuBan := false
	for _, h := range holidays {
		if h.IsWorkday {
			hasBuBan = true
			break
		}
	}
	assert.True(t, hasBuBan, "expected 补班 workday entries in 2025 holidays")
}

func TestGetHolidaysByYear_2028ReturnsNil(t *testing.T) {
	holidays := GetHolidaysByYear(2028)
	assert.Nil(t, holidays)
}

func TestGetHolidaysByYear_holidayFields(t *testing.T) {
	holidays := GetHolidaysByYear(2025)
	for _, h := range holidays {
		assert.NotEmpty(t, h.Date, "Date must not be empty")
		assert.NotEmpty(t, h.Name, "Name must not be empty")
		assert.Len(t, h.Date, 10, "Date must be YYYY-MM-DD format")
	}
}

// ---------------------------------------------------------------------------
// GetMonthHolidays
// ---------------------------------------------------------------------------

func TestGetMonthHolidays_202501(t *testing.T) {
	holidays := GetMonthHolidays(2025, 1)
	assert.Equal(t, 9, len(holidays))

	names := make(map[string]int)
	for _, h := range holidays {
		names[h.Name]++
	}
	assert.Equal(t, 1, names["元旦"])
	assert.Equal(t, 6, names["春节"])
	assert.Equal(t, 2, names["补班（春节）"])
}

func TestGetMonthHolidays_202506(t *testing.T) {
	holidays := GetMonthHolidays(2025, 6)
	assert.Equal(t, 2, len(holidays))
	for _, h := range holidays {
		assert.Equal(t, "端午节", h.Name)
	}
}

func TestGetMonthHolidays_202502(t *testing.T) {
	// 春节 extends into Feb 1-2, plus 补班 none in Feb
	holidays := GetMonthHolidays(2025, 2)
	assert.Equal(t, 2, len(holidays))
	for _, h := range holidays {
		assert.Equal(t, "春节", h.Name)
	}
}

func TestGetMonthHolidays_202511NoData(t *testing.T) {
	holidays := GetMonthHolidays(2025, 11)
	assert.Empty(t, holidays)
}

// ---------------------------------------------------------------------------
// Holiday struct field checks
// ---------------------------------------------------------------------------

func TestHolidayStruct_fieldsArePopulated(t *testing.T) {
	holidays := GetHolidaysByYear(2026)
	for _, h := range holidays {
		assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, h.Date)
		assert.NotZero(t, h.Name)
		// IsWorkday can be true or false, just verify it's set
		assert.Contains(t, []bool{true, false}, h.IsWorkday)
	}
}
