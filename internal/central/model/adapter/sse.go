package adapter

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

type sseReader struct {
	reader *bufio.Reader
}

func (s *sseReader) next() ([]byte, error) {
	var data []byte
	size := 0
	event := ""
	for {
		var line []byte
		for {
			part, err := s.reader.ReadSlice('\n')
			size += len(part)
			if size > maxEventBytes {
				return nil, limitFailure()
			}
			line = append(line, part...)
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			if err != nil {
				return nil, err
			}
			break
		}
		line = bytes.TrimSuffix(line, []byte{'\n'})
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if !utf8.Valid(line) {
			return nil, protocolFailure()
		}
		if len(line) == 0 {
			if event != "" && event != "message" {
				return nil, protocolFailure()
			}
			if len(data) != 0 {
				return bytes.TrimSuffix(data, []byte{'\n'}), nil
			}
			size, event = 0, ""
			continue
		}
		if line[0] == ':' {
			continue
		}
		field, value, _ := bytes.Cut(line, []byte{':'})
		value = bytes.TrimPrefix(value, []byte{' '})
		switch string(field) {
		case "data":
			data = append(data, value...)
			data = append(data, '\n')
		case "event":
			event = string(value)
		case "id", "retry": // Transport metadata is not a Model call identity.
		default:
			return nil, protocolFailure()
		}
	}
}

func (x *Exchange) stream(body io.Reader) (End, error) {
	r := sseReader{reader: bufio.NewReaderSize(body, 32<<10)}
	end := End{Usage: mc.Usage{Source: mc.UnknownUsage}}
	finished, usageSeen := false, false
	textBytes := 0
	// Only structured output retains cumulative content. String() borrows this
	// bounded builder; validation never makes a second whole-content copy/DOM.
	var structuredText strings.Builder
	for {
		if err := x.ctx.Err(); err != nil {
			return End{}, contextFailure(err)
		}
		raw, err := r.next()
		if err != nil {
			x.observeNetworkError(err)
			if err == io.EOF {
				return End{}, protocolFailure()
			}
			var protocol *mc.ModelError
			if errors.As(err, &protocol) {
				return End{}, err
			}
			return End{}, transportFailure(err)
		}
		if bytes.Equal(raw, []byte("[DONE]")) {
			if !finished {
				return End{}, protocolFailure()
			}
			if x.schema != nil {
				if err := x.schema.complete(x.ctx, structuredText.String(), end.FinishReason); err != nil {
					return End{}, err
				}
			}
			end.ProviderRequestID = x.requestID
			return end, nil
		}
		value, err := decodeNative(raw, true)
		if err != nil {
			return End{}, err
		}
		if value.emptyChoices {
			if !finished || usageSeen || !value.hasUsage {
				return End{}, protocolFailure()
			}
			usageSeen = true
			end.Usage = value.usage.Clone()
			x.observeUsage(value.usage)
			if err := x.emit(Event{Kind: UsageUpdate, Usage: &value.usage}); err != nil {
				return End{}, err
			}
			continue
		}
		if finished || value.hasUsage {
			return End{}, protocolFailure()
		}
		if value.refusal {
			return End{}, failure("content_filter", "")
		}
		textBytes += len(value.text)
		if textBytes > maxTextBytes {
			return End{}, limitFailure()
		}
		if x.schema != nil {
			structuredText.WriteString(value.text)
		}
		for remaining := value.text; remaining != ""; {
			n := min(len(remaining), 64<<10)
			if n < len(remaining) {
				for n > 0 && !utf8.RuneStart(remaining[n]) {
					n--
				}
			}
			if n == 0 {
				return End{}, protocolFailure()
			}
			if err := x.emit(Event{Kind: TextDelta, Text: strings.Clone(remaining[:n])}); err != nil {
				return End{}, err
			}
			remaining = remaining[n:]
		}
		if value.finish != "" {
			finished = true
			end.FinishReason = value.finish
		}
	}
}
