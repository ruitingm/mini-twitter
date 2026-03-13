package logger

import (
	"os"

	"github.com/rs/zerolog"
)

// New creates a structured JSON logger that writes to stdout.
// Every log line includes a timestamp and the service name for easy filtering in log aggregators.
func New(service string) zerolog.Logger {
	return zerolog.New(os.Stdout). // write JSON log lines to stdout
		With().
		Timestamp().      // add a "time" field to every log entry
		Str("service", service). // tag every entry with the microservice name
		Logger()
}
