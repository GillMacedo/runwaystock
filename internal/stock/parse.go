package stock

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func decode(data []byte) string {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if utf8.Valid(data) {
		return string(data)
	}
	runes := make([]rune, len(data))
	for i, b := range data {
		runes[i] = rune(b)
	}
	return string(runes)
}

func detectDelimiter(text string) rune {
	lines := strings.Split(text, "\n")
	if len(lines) > 30 {
		lines = lines[:30]
	}
	best, bestCount := ',', -1
	for _, delimiter := range []rune{',', ';', '\t', '|'} {
		count := 0
		for _, line := range lines {
			count += strings.Count(line, string(delimiter))
		}
		if count > bestCount {
			best, bestCount = delimiter, count
		}
	}
	return best
}

var (
	amountNoise = regexp.MustCompile(`[A-Za-z$€£¥\s\x{00a0}']`)
	amountShape = regexp.MustCompile(`^[\d.,]*\d[\d.,]*$`)
	decimalPart = regexp.MustCompile(`[.,](\d{1,2})$`)
)

// ParseAmount returns cents. The last separator is the decimal mark only if one
// or two digits follow it, so both 1,234.56 and 1.234,56 work.
func ParseAmount(raw string) (int64, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return 0, false
	}
	negative := false
	if strings.HasPrefix(text, "(") && strings.HasSuffix(text, ")") {
		negative, text = true, text[1:len(text)-1]
	}
	text = amountNoise.ReplaceAllString(text, "")
	if strings.HasPrefix(text, "-") || strings.HasPrefix(text, "−") {
		negative, text = !negative, strings.TrimLeft(text, "-−")
	} else if strings.HasSuffix(text, "-") {
		negative, text = !negative, strings.TrimRight(text, "-")
	}
	if !amountShape.MatchString(text) {
		return 0, false
	}
	fraction, whole := "", text
	if match := decimalPart.FindStringSubmatch(text); match != nil {
		fraction, whole = match[1], text[:len(text)-len(match[0])]
	}
	whole = strings.NewReplacer(".", "", ",", "").Replace(whole)
	if whole == "" {
		whole = "0"
	}
	units, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || units > 1<<52 {
		return 0, false
	}
	cents := units * 100
	if fraction != "" {
		value, _ := strconv.ParseInt((fraction + "0")[:2], 10, 64)
		cents += value
	}
	if negative {
		cents = -cents
	}
	return cents, true
}

var slashed = regexp.MustCompile(`^(\d{1,2})[/.\-](\d{1,2})[/.\-](\d{2}|\d{4})$`)
var isoDate = regexp.MustCompile(`^(\d{4})[/.\-](\d{1,2})[/.\-](\d{1,2})(?:[T ].*)?$`)

// DetectDateFormat reads the whole column. If every date could be day-first or
// month-first it returns "", because a guess would shift dates silently.
func DetectDateFormat(values []string) string {
	dayFirst, monthFirst, any := false, false, false
	for _, value := range values {
		match := slashed.FindStringSubmatch(strings.TrimSpace(value))
		if match == nil {
			continue
		}
		any = true
		first, _ := strconv.Atoi(match[1])
		second, _ := strconv.Atoi(match[2])
		dayFirst = dayFirst || first > 12
		monthFirst = monthFirst || second > 12
	}
	switch {
	case !any:
		return "ymd"
	case dayFirst == monthFirst:
		return ""
	case dayFirst:
		return "dmy"
	}
	return "mdy"
}

// ParseDate returns false for anything that isn't a real date.
func ParseDate(raw, format string) (time.Time, bool) {
	text := strings.TrimSpace(raw)
	var year, month, day int
	if match := isoDate.FindStringSubmatch(text); match != nil {
		year, _ = strconv.Atoi(match[1])
		month, _ = strconv.Atoi(match[2])
		day, _ = strconv.Atoi(match[3])
	} else if match := slashed.FindStringSubmatch(text); match != nil && (format == "dmy" || format == "mdy") {
		first, _ := strconv.Atoi(match[1])
		second, _ := strconv.Atoi(match[2])
		year, _ = strconv.Atoi(match[3])
		if year < 100 {
			year += 2000
		}
		day, month = first, second
		if format == "mdy" {
			day, month = second, first
		}
	} else {
		return time.Time{}, false
	}
	date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if date.Year() != year || int(date.Month()) != month || date.Day() != day {
		return time.Time{}, false
	}
	return date, true
}
