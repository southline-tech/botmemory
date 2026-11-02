package usage

import (
	"strings"

	"github.com/southline-tech/botoptimizer/internal/vault"
)

func LogKey(bot string) (string, error) {
	if err := vault.ValidName("bot", bot); err != nil {
		return "", err
	}
	return "log.usage." + bot, nil
}

func ShareKey(date string) (string, error) {
	if _, err := parseDateOnly(date); err != nil {
		return "", err
	}
	return "usage.share." + date, nil
}

func CSVKey(date string) (string, error) {
	if _, err := parseDateOnly(date); err != nil {
		return "", err
	}
	return "usage.csv." + date, nil
}

func WatchRulesKey() string { return "watch.rules" }

func csvDedupeKey(date string) string { return "usage.csv.dedupe." + date }

func prefixLogUsage(key string) bool { return strings.HasPrefix(key, "log.usage.") }

func botFromLogKey(key string) string {
	return strings.TrimPrefix(key, "log.usage.")
}
