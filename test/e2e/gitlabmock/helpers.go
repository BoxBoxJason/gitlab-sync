package gitlabmock

import (
	"io"
	"log"
)

// closeLogged closes closer. A failure is logged: nothing else can be done
// about it.
func closeLogged(closer io.Closer) {
	logFailure("close", closer.Close())
}

// logFailure logs err, when there is one, as a failure of what.
func logFailure(what string, err error) {
	if err != nil {
		log.Printf("gitlabmock: %s: %v", what, err)
	}
}

// sendBytes writes data to the client. A failed write only means the client went
// away, so it is logged and not reported to the caller.
func sendBytes(writer io.Writer, data []byte) {
	_, err := writer.Write(data)

	logFailure("write the response", err)
}
