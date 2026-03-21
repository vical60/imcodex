package lark

import (
	"context"
	"testing"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestClientListChatMessagesSinceSkipsDecodeErrors(t *testing.T) {
	t.Parallel()

	client := &Client{
		listMessages: func(context.Context, *larkim.ListMessageReq, ...larkcore.RequestOptionFunc) (*larkim.ListMessageResp, error) {
			hasMore := false
			return &larkim.ListMessageResp{
				Data: &larkim.ListMessageRespData{
					HasMore: &hasMore,
					Items: []*larkim.Message{
						{
							MessageId: stringPtr("om_bad"),
							ChatId:    stringPtr("oc_1"),
							MsgType:   stringPtr("text"),
							Sender:    &larkim.Sender{SenderType: stringPtr("user")},
							Body:      &larkim.MessageBody{Content: stringPtr("{")},
						},
						{
							MessageId:  stringPtr("om_good"),
							ChatId:     stringPtr("oc_1"),
							MsgType:    stringPtr("text"),
							CreateTime: stringPtr("123456"),
							Sender:     &larkim.Sender{SenderType: stringPtr("user")},
							Body:       &larkim.MessageBody{Content: stringPtr(`{"text":"hello"}`)},
						},
					},
				},
			}, nil
		},
	}

	listed, err := client.ListChatMessagesSince(context.Background(), "oc_1", 0)
	if err != nil {
		t.Fatalf("ListChatMessagesSince() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("len(listed) = %d, want 1", len(listed))
	}
	if got := listed[0].Message.MessageID; got != "om_good" {
		t.Fatalf("listed[0].Message.MessageID = %q, want om_good", got)
	}
}
