// Package schedule reads a routine's schedule: "every 30m", "every 2h",
// "every 1d", or five cron fields ("0 9 * * 1-5") in an IANA timezone, and
// finds its next run.
package schedule

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // the IANA rules, also where the system has none (Windows)
)

// MinInterval is the shortest gap a schedule may have.
const MinInterval = 5 * time.Minute

// Schedule is a parsed schedule.
type Schedule struct {
	Every time.Duration // an interval; zero for cron
	cron  *cron
	Loc   *time.Location
	text  string
}

// Parse reads spec in the timezone tz ("" is the local one).
func Parse(spec, tz string) (*Schedule, error) {
	loc, err := Location(tz)
	if err != nil {
		return nil, err
	}
	spec = strings.TrimSpace(strings.ToLower(spec))
	s := &Schedule{Loc: loc, text: spec}
	if rest, ok := strings.CutPrefix(spec, "every "); ok {
		d, err := interval(strings.TrimSpace(rest))
		if err != nil {
			return nil, err
		}
		if d < MinInterval {
			return nil, fmt.Errorf("every %s is too often: at most every five minutes", rest)
		}
		s.Every = d
		return s, nil
	}
	c, err := parseCron(spec)
	if err != nil {
		return nil, err
	}
	s.cron = c
	if gap := c.minGap(); gap < MinInterval {
		return nil, fmt.Errorf("%q runs too often: at most every five minutes", spec)
	}
	return s, nil
}

func interval(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("bad interval %q (every 30m, every 2h, every 1d)", s)
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("bad interval %q (every 30m, every 2h, every 1d)", s)
	}
	switch s[len(s)-1] {
	case 'm':
		return time.Duration(n) * time.Minute, nil
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	}
	return 0, fmt.Errorf("bad interval %q (every 30m, every 2h, every 1d)", s)
}

// Next is the first run after t. An interval counts from t, the latest run
// or the time it was made.
func (s *Schedule) Next(t time.Time) time.Time {
	if s.Every > 0 {
		return t.Add(s.Every)
	}
	return s.cron.next(t.In(s.Loc))
}

// Location loads an IANA timezone; "" is the local one.
func Location(tz string) (*time.Location, error) {
	if tz == "" {
		tz = LocalZone()
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("unknown timezone %q", tz)
	}
	return loc, nil
}

// LocalZone is the IANA name of this computer's timezone, UTC when unknown.
func LocalZone() string {
	if tz := os.Getenv("TZ"); tz != "" && !strings.HasPrefix(tz, ":") {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	if p, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if i := strings.Index(p, "zoneinfo/"); i >= 0 {
			name := p[i+len("zoneinfo/"):]
			if _, err := time.LoadLocation(name); err == nil {
				return name
			}
		}
	}
	if n := time.Local.String(); n != "Local" && n != "" {
		return n
	}
	return "UTC"
}

// ---- cron ----

type cron struct {
	min, hour, dom, month, dow [64]bool
	domAny, dowAny             bool
	fields                     []string
}

var fieldRanges = [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}

var names = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

func parseCron(spec string) (*cron, error) {
	f := strings.Fields(spec)
	if len(f) != 5 {
		return nil, fmt.Errorf("bad schedule %q: every 30m, every 2h, every 1d, or five cron fields (0 9 * * 1-5)", spec)
	}
	c := &cron{fields: f, domAny: f[2] == "*", dowAny: f[4] == "*"}
	sets := []*[64]bool{&c.min, &c.hour, &c.dom, &c.month, &c.dow}
	for i, field := range f {
		if err := parseField(field, fieldRanges[i][0], fieldRanges[i][1], sets[i]); err != nil {
			return nil, fmt.Errorf("bad schedule %q: %v", spec, err)
		}
	}
	if c.dow[7] {
		c.dow[0] = true // 7 is Sunday too
	}
	return c, nil
}

func parseField(field string, lo, hi int, set *[64]bool) error {
	for _, part := range strings.Split(field, ",") {
		step := 1
		if base, s, ok := strings.Cut(part, "/"); ok {
			n, err := strconv.Atoi(s)
			if err != nil || n <= 0 {
				return fmt.Errorf("bad step in %q", part)
			}
			step, part = n, base
		}
		from, to := lo, hi
		if part != "*" {
			a, b, isRange := strings.Cut(part, "-")
			x, err := value(a)
			if err != nil {
				return err
			}
			from, to = x, x
			if isRange {
				if to, err = value(b); err != nil {
					return err
				}
			} else if step > 1 {
				to = hi
			}
		}
		if from < lo || to > hi || from > to {
			return fmt.Errorf("%q is out of %d-%d", part, lo, hi)
		}
		for v := from; v <= to; v += step {
			set[v] = true
		}
	}
	return nil
}

func value(s string) (int, error) {
	if n, ok := names[s]; ok {
		return n, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("bad value %q", s)
	}
	return n, nil
}

func (c *cron) dayMatches(t time.Time) bool {
	dom, dow := c.dom[t.Day()], c.dow[int(t.Weekday())]
	switch {
	case c.domAny && c.dowAny:
		return true
	case c.domAny:
		return dow
	case c.dowAny:
		return dom
	}
	return dom || dow // both restricted: either, as cron does
}

// next is the first matching minute after t, in t's location. A minute that
// daylight saving skips does not fire that day.
func (c *cron) next(t time.Time) time.Time {
	loc := t.Location()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	for i := 0; i < 366*5; i++ {
		d := day.AddDate(0, 0, i)
		if !c.month[int(d.Month())] || !c.dayMatches(d) {
			continue
		}
		for h := 0; h < 24; h++ {
			if !c.hour[h] {
				continue
			}
			for m := 0; m < 60; m++ {
				if !c.min[m] {
					continue
				}
				at := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, loc)
				if at.Hour() != h || at.Minute() != m || at.Day() != d.Day() {
					continue // skipped by daylight saving
				}
				if at.After(t) {
					return at
				}
			}
		}
	}
	return time.Time{}
}

// minGap is the shortest gap between two runs within a day's minutes.
func (c *cron) minGap() time.Duration {
	var mins []int
	for h := 0; h < 24; h++ {
		if !c.hour[h] {
			continue
		}
		for m := 0; m < 60; m++ {
			if c.min[m] {
				mins = append(mins, h*60+m)
			}
		}
	}
	if len(mins) < 2 {
		return 24 * time.Hour
	}
	gap := 24 * 60
	for i := 1; i < len(mins); i++ {
		gap = min(gap, mins[i]-mins[i-1])
	}
	gap = min(gap, mins[0]+24*60-mins[len(mins)-1])
	return time.Duration(gap) * time.Minute
}

// ErrNever is returned for a cron spec that never fires.
var ErrNever = errors.New("the schedule never runs")

// Text says the schedule in words: "Every 2 hours", "Weekdays at 9:00",
// "Every day at 8:30", "On the 1st of every month at 8:30", else "Cron …".
func (s *Schedule) Text() string {
	if s.Every > 0 {
		switch d := s.Every; {
		case d%(7*24*time.Hour) == 0:
			return plural(int(d/(7*24*time.Hour)), "week")
		case d%(24*time.Hour) == 0:
			return plural(int(d/(24*time.Hour)), "day")
		case d%time.Hour == 0:
			return plural(int(d/time.Hour), "hour")
		default:
			return plural(int(d/time.Minute), "minute")
		}
	}
	f := s.cron.fields
	m, errM := strconv.Atoi(f[0])
	h, errH := strconv.Atoi(f[1])
	if errM != nil || errH != nil || f[3] != "*" {
		return "Cron " + strings.Join(f, " ")
	}
	at := fmt.Sprintf("%d:%02d", h, m)
	switch {
	case f[2] == "*" && f[4] == "*":
		return "Every day at " + at
	case f[2] == "*" && (f[4] == "1-5" || f[4] == "mon-fri"):
		return "Weekdays at " + at
	case f[2] == "*" && (f[4] == "0,6" || f[4] == "6,0" || f[4] == "sat,sun"):
		return "Weekends at " + at
	case f[2] == "*":
		if d, err := value(f[4]); err == nil && d >= 0 && d <= 7 {
			return "Every " + time.Weekday(d%7).String() + " at " + at
		}
	case f[4] == "*":
		if d, err := strconv.Atoi(f[2]); err == nil {
			return "On the " + ordinal(d) + " of every month at " + at
		}
	}
	return "Cron " + strings.Join(f, " ")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "Every " + unit
	}
	return fmt.Sprintf("Every %d %ss", n, unit)
}

func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return strconv.Itoa(n) + suffix
}
