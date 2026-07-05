package sms

import "fmt"

// Message wording is a placeholder pending the copywriting fast-follow noted
// in docs/PLAN.md — structure (bilingual KK+RU, two lines) is fixed, exact
// phrasing is not.

func PositionMessage(aheadCount int) string {
	return fmt.Sprintf(
		"KK: Sizge dейin %d adam bar.\nRU: Перед вами %d человек.",
		aheadCount, aheadCount,
	)
}

func YourTurnMessage() string {
	return "KK: Sizdin kezekiniz keldi.\nRU: Подошла ваша очередь."
}

func QueueStartedMessage(position int) string {
	return fmt.Sprintf(
		"KK: Kezek bastaldy. Sizdin ornynyz: %d.\nRU: Очередь началась. Ваша позиция: %d.",
		position, position,
	)
}

func QueueClosedMessage() string {
	return "KK: Bugin kabyldau ayaktaldy. Sizdin ornynyz saktaldy.\nRU: Приём на сегодня закрыт. Ваше место в очереди сохранено."
}
