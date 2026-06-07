package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpsServiceRecordErrorBatch_SanitizesAndBatches(t *testing.T) {
	t.Parallel()

	var captured []*OpsInsertErrorLogInput
	repo := &opsRepoMock{
		BatchInsertErrorLogsFn: func(ctx context.Context, inputs []*OpsInsertErrorLogInput) (int64, error) {
			captured = append(captured, inputs...)
			return int64(len(inputs)), nil
		},
	}
	svc := NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	msg := " upstream failed: https://example.com?access_token=secret-value "
	detail := `{"authorization":"Bearer secret-token"}`
	entries := []*OpsInsertErrorLogInput{
		{
			ErrorMessage:         "Authorization: Bearer sk-secret-token access_token=plain-secret",
			ErrorBody:            `{"error":"bad","access_token":"secret"}`,
			UpstreamStatusCode:   intPtr(-10),
			UpstreamErrorMessage: strPtr(msg),
			UpstreamErrorDetail:  strPtr(detail),
			NetworkErrorType:     " request_body_timeout ",
			UpstreamErrors: []*OpsUpstreamErrorEvent{
				{
					AccountID:            -2,
					UpstreamStatusCode:   429,
					Message:              " token leaked ",
					Detail:               `{"refresh_token":"secret"}`,
					UpstreamRequestBody:  `{"api_key":"sk-secret","messages":[{"role":"user","content":"hello"}]}`,
					UpstreamResponseBody: `{"access_token":"secret","error":"bad"}`,
				},
			},
		},
		{
			ErrorPhase: "upstream",
			ErrorType:  "upstream_error",
			CreatedAt:  time.Now().UTC(),
		},
	}

	require.NoError(t, svc.RecordErrorBatch(context.Background(), entries))
	require.Len(t, captured, 2)

	first := captured[0]
	require.Equal(t, "internal", first.ErrorPhase)
	require.Equal(t, "api_error", first.ErrorType)
	require.NotContains(t, first.ErrorMessage, "sk-secret-token")
	require.NotContains(t, first.ErrorMessage, "plain-secret")
	require.Nil(t, first.UpstreamStatusCode)
	require.NotNil(t, first.UpstreamErrorMessage)
	require.NotContains(t, *first.UpstreamErrorMessage, "secret-value")
	require.Contains(t, *first.UpstreamErrorMessage, "access_token=***")
	require.NotNil(t, first.UpstreamErrorDetail)
	require.NotContains(t, *first.UpstreamErrorDetail, "secret-token")
	require.NotContains(t, first.ErrorBody, "secret")
	require.Equal(t, "request_body_timeout", first.NetworkErrorType)
	require.Nil(t, first.UpstreamErrors)
	require.NotNil(t, first.UpstreamErrorsJSON)
	require.NotContains(t, *first.UpstreamErrorsJSON, "secret")
	require.Contains(t, *first.UpstreamErrorsJSON, "[REDACTED]")
	require.Contains(t, *first.UpstreamErrorsJSON, "upstream_request_body")
	require.Contains(t, *first.UpstreamErrorsJSON, "upstream_response_body")

	second := captured[1]
	require.Equal(t, "upstream", second.ErrorPhase)
	require.Equal(t, "upstream_error", second.ErrorType)
	require.False(t, second.CreatedAt.IsZero())
}

func TestOpsServiceRecordErrorBatch_FallsBackToSingleInsert(t *testing.T) {
	t.Parallel()

	var (
		batchCalls  int
		singleCalls int
	)
	repo := &opsRepoMock{
		BatchInsertErrorLogsFn: func(ctx context.Context, inputs []*OpsInsertErrorLogInput) (int64, error) {
			batchCalls++
			return 0, errors.New("batch failed")
		},
		InsertErrorLogFn: func(ctx context.Context, input *OpsInsertErrorLogInput) (int64, error) {
			singleCalls++
			return int64(singleCalls), nil
		},
	}
	svc := NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	err := svc.RecordErrorBatch(context.Background(), []*OpsInsertErrorLogInput{
		{ErrorMessage: "first"},
		{ErrorMessage: "second"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, batchCalls)
	require.Equal(t, 2, singleCalls)
}

func strPtr(v string) *string {
	return &v
}
