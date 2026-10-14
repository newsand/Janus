package mailer

import "github.com/newsand/janus/internal/logger"

type stubSender struct{}

func (stubSender) Send(p_Msg Message) error {
	logger.Info("[MAILER STUB] to=%s subject=%q\n%s", p_Msg.To, p_Msg.Subject, p_Msg.Body)
	return nil
}
