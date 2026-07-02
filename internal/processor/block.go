// Copyright 2026 ICAP Mock

package processor

import "github.com/icap-mock/icap-mock/internal/storage"

const (
	streamFinishFINMode      = "fin"
	streamFinishTermMode     = "term"
	streamFinishWeightedMode = "weighted"
	statusCodeBlockMin       = 400
	statusCodeBlockMax       = 599
)

func responseBlocks(response *storage.ResponseTemplate) bool {
	if response == nil {
		return false
	}
	if response.Block != nil {
		return *response.Block
	}
	return responseAutoBlocks(response)
}

func responseAutoBlocks(response *storage.ResponseTemplate) bool {
	return isBlockStatus(responseStatusCode(response)) ||
		isBlockStatus(response.HTTPStatus) ||
		streamAutoBlocks(response.Stream) ||
		response.Error != ""
}

func streamAutoBlocks(stream *storage.StreamConfig) bool {
	if stream == nil {
		return false
	}
	finish := stream.Finish
	return stream.End.Mode == streamFinishFINMode ||
		stream.End.Mode == streamFinishTermMode ||
		finish.Mode == streamFinishFINMode ||
		finish.Mode == streamFinishTermMode ||
		(finish.Mode == streamFinishWeightedMode && finish.FinPercent > 0)
}

func isBlockStatus(status int) bool {
	return status >= statusCodeBlockMin && status <= statusCodeBlockMax
}
