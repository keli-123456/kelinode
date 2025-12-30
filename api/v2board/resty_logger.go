package panel

import "github.com/go-resty/resty/v2"

// silentRestyLogger suppresses Resty's internal logger output (e.g. "WARN RESTY ..."),
// so we keep logs consistent via logrus at the application layer.
type silentRestyLogger struct{}

var _ resty.Logger = (*silentRestyLogger)(nil)

func (*silentRestyLogger) Errorf(string, ...interface{}) {}
func (*silentRestyLogger) Warnf(string, ...interface{})  {}
func (*silentRestyLogger) Debugf(string, ...interface{}) {}
