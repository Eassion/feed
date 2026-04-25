package gosafe

import "log/slog"

func Go(logger *slog.Logger, fn func()) {
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil && logger != nil {
				logger.Error("goroutine panic recovered", "panic", recovered)
			}
		}()

		fn()
	}()
}
