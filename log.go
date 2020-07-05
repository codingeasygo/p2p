package p2p

import (
	"fmt"

	log "github.com/sirupsen/logrus"
)

//PlainFormatter is logrus formatter
type PlainFormatter struct {
	TimestampFormat string
	LevelDesc       []string
}

//NewPlainFormatter will create new formater
func NewPlainFormatter() (formatter *PlainFormatter) {
	formatter = &PlainFormatter{
		TimestampFormat: "2006-01-02 15:04:05",
		LevelDesc:       []string{"PANC", "FATL", "ERRO", "WARN", "INFO", "DEBG"},
	}
	return
}

//Format will format the log entry
func (f *PlainFormatter) Format(entry *log.Entry) ([]byte, error) {
	timestamp := fmt.Sprintf(entry.Time.Format(f.TimestampFormat))
	return []byte(fmt.Sprintf("%s %s %s\n", f.LevelDesc[entry.Level], timestamp, entry.Message)), nil
}
