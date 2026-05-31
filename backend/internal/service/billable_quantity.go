package service

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

const (
	defaultDurationSeconds = 1
	defaultCharacterCount  = 1000
)

func extractBillableDurationSeconds(respBody, requestBody []byte, contentType string, forceFallback bool) (int, bool) {
	if seconds, found := extractResponseDurationSeconds(respBody); found {
		if seconds > 0 {
			return seconds, true
		}
		forceFallback = true
	}
	if seconds, ok := extractAudioDurationFromRequest(requestBody, contentType); ok && seconds > 0 {
		return seconds, true
	}
	if forceFallback {
		return defaultDurationSeconds, true
	}
	return 0, false
}

func extractBillableCharacterCount(requestBody []byte, contentType string, forceFallback bool) (int, bool) {
	count := countTextCharactersFromRequest(requestBody, contentType)
	if count > 0 {
		return count, true
	}
	if forceFallback {
		return defaultCharacterCount, true
	}
	return 0, false
}

func extractResponseDurationSeconds(body []byte) (int, bool) {
	if !json.Valid(body) {
		return 0, false
	}
	for _, path := range []string{"usage.seconds", "usage.duration_seconds", "duration_seconds", "duration"} {
		value := gjson.GetBytes(body, path)
		if !value.Exists() {
			continue
		}
		if seconds := ceilPositiveSeconds(value.Float()); seconds > 0 {
			return seconds, true
		}
		return 0, true
	}
	return 0, false
}

func ceilPositiveSeconds(value float64) int {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return int(math.Ceil(value))
}

func extractAudioDurationFromRequest(body []byte, contentType string) (int, bool) {
	if len(body) == 0 {
		return 0, false
	}
	if isAudioContentType(contentType) || looksLikeAudioData(body) {
		if seconds, ok := parseAudioDurationSeconds(body); ok {
			return seconds, true
		}
	}
	if seconds, ok := extractAudioDurationFromMultipart(body, contentType); ok {
		return seconds, true
	}
	if seconds, ok := extractAudioDurationFromJSON(body); ok {
		return seconds, true
	}
	return 0, false
}

func extractAudioDurationFromMultipart(body []byte, contentType string) (int, bool) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		return 0, false
	}
	boundary := params["boundary"]
	if boundary == "" {
		return 0, false
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return 0, false
		}
		if err != nil {
			return 0, false
		}
		data, _ := io.ReadAll(part)
		if len(data) == 0 {
			continue
		}
		if part.FileName() == "" && !isAudioCandidateKey(part.FormName()) && !isAudioContentType(part.Header.Get("Content-Type")) {
			continue
		}
		if seconds, ok := parseAudioDurationSeconds(data); ok {
			return seconds, true
		}
	}
}

func extractAudioDurationFromJSON(body []byte) (int, bool) {
	if !json.Valid(body) {
		return 0, false
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return 0, false
	}
	return walkAudioCandidates(value, "")
}

func walkAudioCandidates(value any, key string) (int, bool) {
	switch v := value.(type) {
	case map[string]any:
		for k, child := range v {
			if seconds, ok := walkAudioCandidates(child, k); ok {
				return seconds, true
			}
		}
	case []any:
		for _, child := range v {
			if seconds, ok := walkAudioCandidates(child, key); ok {
				return seconds, true
			}
		}
	case string:
		if data, ok := decodeAudioStringCandidate(v, key); ok {
			if seconds, parsed := parseAudioDurationSeconds(data); parsed {
				return seconds, true
			}
		}
	}
	return 0, false
}

func decodeAudioStringCandidate(raw string, key string) ([]byte, bool) {
	value := strings.TrimSpace(raw)
	if value == "" || looksLikeURL(value) {
		return nil, false
	}
	if strings.HasPrefix(strings.ToLower(value), "data:") {
		return decodeAudioDataURLCandidate(value)
	}
	if len(value) < 64 || (!isAudioCandidateKey(key) && !looksLikeAudioBase64Prefix(value)) {
		return nil, false
	}
	compact := compactBase64(value)
	if compact == "" || len(compact)%4 == 1 {
		return nil, false
	}
	decoded, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(compact, "="))
	}
	if err != nil || !looksLikeAudioData(decoded) {
		return nil, false
	}
	return decoded, true
}

func decodeAudioDataURLCandidate(value string) ([]byte, bool) {
	comma := strings.IndexByte(value, ',')
	if comma <= 0 {
		return nil, false
	}
	meta := strings.ToLower(value[:comma])
	if !strings.Contains(meta, "audio/") && !strings.Contains(meta, "application/octet-stream") {
		return nil, false
	}
	payload := value[comma+1:]
	if strings.Contains(meta, ";base64") {
		decoded, err := base64.StdEncoding.DecodeString(compactBase64(payload))
		if err != nil {
			return nil, false
		}
		return decoded, true
	}
	decoded, err := url.QueryUnescape(payload)
	if err != nil {
		return nil, false
	}
	return []byte(decoded), true
}

func compactBase64(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch r {
		case ' ', '\n', '\r', '\t':
			continue
		default:
			_, _ = b.WriteRune(r)
		}
	}
	return b.String()
}

func looksLikeAudioBase64Prefix(value string) bool {
	compact := compactBase64(value)
	if len(compact) < 16 {
		return false
	}
	prefix := compact
	if len(prefix) > 64 {
		prefix = prefix[:64]
	}
	decoded, err := base64.StdEncoding.DecodeString(prefix + strings.Repeat("=", (4-len(prefix)%4)%4))
	if err != nil {
		return false
	}
	return looksLikeAudioData(decoded)
}

func isAudioCandidateKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return key == "file" ||
		key == "audio" ||
		key == "data" ||
		key == "input_audio" ||
		key == "audio_data" ||
		strings.Contains(key, "audio")
}

func isAudioContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	return strings.HasPrefix(mediaType, "audio/") ||
		mediaType == "application/octet-stream"
}

func looksLikeAudioData(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	return bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WAVE")) ||
		bytes.HasPrefix(data, []byte("ID3")) ||
		(data[0] == 0xff && data[1]&0xe0 == 0xe0) ||
		bytes.Equal(data[4:8], []byte("ftyp"))
}

func parseAudioDurationSeconds(data []byte) (int, bool) {
	switch {
	case len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WAVE")):
		return parseWAVDurationSeconds(data)
	case len(data) >= 10 && bytes.HasPrefix(data, []byte("ID3")), len(data) >= 2 && data[0] == 0xff && data[1]&0xe0 == 0xe0:
		return parseMP3DurationSeconds(data)
	case len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp")):
		return parseMP4DurationSeconds(data)
	default:
		return 0, false
	}
}

func parseWAVDurationSeconds(data []byte) (int, bool) {
	var byteRate uint32
	var dataSize uint32
	for offset := 12; offset+8 <= len(data); {
		chunkID := string(data[offset : offset+4])
		chunkSize := binary.LittleEndian.Uint32(data[offset+4 : offset+8])
		payload := offset + 8
		next := payload + int(chunkSize)
		if next > len(data) {
			return 0, false
		}
		switch chunkID {
		case "fmt ":
			if chunkSize >= 16 {
				byteRate = binary.LittleEndian.Uint32(data[payload+8 : payload+12])
			}
		case "data":
			dataSize = chunkSize
		}
		if byteRate > 0 && dataSize > 0 {
			return ceilPositiveSeconds(float64(dataSize) / float64(byteRate)), true
		}
		offset = next + int(chunkSize%2)
	}
	return 0, false
}

func parseMP3DurationSeconds(data []byte) (int, bool) {
	offset := skipID3v2(data)
	totalSeconds := 0.0
	frames := 0
	for offset+4 <= len(data) {
		frameLen, samples, sampleRate, ok := parseMP3FrameHeader(data[offset : offset+4])
		if !ok || frameLen <= 0 || offset+frameLen > len(data) {
			offset++
			continue
		}
		totalSeconds += float64(samples) / float64(sampleRate)
		frames++
		offset += frameLen
	}
	if frames == 0 {
		return 0, false
	}
	return ceilPositiveSeconds(totalSeconds), true
}

func skipID3v2(data []byte) int {
	if len(data) < 10 || !bytes.HasPrefix(data, []byte("ID3")) {
		return 0
	}
	size := int(data[6]&0x7f)<<21 | int(data[7]&0x7f)<<14 | int(data[8]&0x7f)<<7 | int(data[9]&0x7f)
	if 10+size >= len(data) {
		return 0
	}
	return 10 + size
}

func parseMP3FrameHeader(h []byte) (frameLen int, samples int, sampleRate int, ok bool) {
	if len(h) < 4 || h[0] != 0xff || h[1]&0xe0 != 0xe0 {
		return 0, 0, 0, false
	}
	versionID := (h[1] >> 3) & 0x03
	layerID := (h[1] >> 1) & 0x03
	bitrateIdx := (h[2] >> 4) & 0x0f
	sampleIdx := (h[2] >> 2) & 0x03
	padding := int((h[2] >> 1) & 0x01)
	if versionID == 1 || layerID == 0 || bitrateIdx == 0 || bitrateIdx == 15 || sampleIdx == 3 {
		return 0, 0, 0, false
	}
	sampleRate = mp3SampleRate(versionID, sampleIdx)
	bitrate := mp3BitrateKbps(versionID, layerID, bitrateIdx)
	if sampleRate <= 0 || bitrate <= 0 {
		return 0, 0, 0, false
	}
	samples = mp3SamplesPerFrame(versionID, layerID)
	if layerID == 3 {
		frameLen = (12*bitrate*1000/sampleRate + padding) * 4
	} else if layerID == 1 && versionID != 3 {
		frameLen = 72*bitrate*1000/sampleRate + padding
	} else {
		frameLen = 144*bitrate*1000/sampleRate + padding
	}
	return frameLen, samples, sampleRate, true
}

func mp3SampleRate(versionID byte, sampleIdx byte) int {
	base := []int{44100, 48000, 32000}[sampleIdx]
	switch versionID {
	case 3:
		return base
	case 2:
		return base / 2
	case 0:
		return base / 4
	default:
		return 0
	}
}

func mp3BitrateKbps(versionID, layerID, idx byte) int {
	tables := map[byte]map[byte][]int{
		3: {
			3: {0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448, 0},
			2: {0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, 0},
			1: {0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
		},
		2: {
			3: {0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256, 0},
			2: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
			1: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
		},
		0: {
			3: {0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256, 0},
			2: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
			1: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
		},
	}
	return tables[versionID][layerID][idx]
}

func mp3SamplesPerFrame(versionID, layerID byte) int {
	switch layerID {
	case 3:
		return 384
	case 2:
		return 1152
	case 1:
		if versionID == 3 {
			return 1152
		}
		return 576
	default:
		return 0
	}
}

func parseMP4DurationSeconds(data []byte) (int, bool) {
	duration, ok := scanMP4Duration(data, 0)
	if !ok {
		return 0, false
	}
	return ceilPositiveSeconds(duration), true
}

func scanMP4Duration(data []byte, depth int) (float64, bool) {
	if depth > 8 {
		return 0, false
	}
	for offset := 0; offset+8 <= len(data); {
		size := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
		boxType := string(data[offset+4 : offset+8])
		header := 8
		switch size {
		case 1:
			if offset+16 > len(data) {
				return 0, false
			}
			size = binary.BigEndian.Uint64(data[offset+8 : offset+16])
			header = 16
		case 0:
			size = uint64(len(data) - offset)
		}
		if size < uint64(header) || offset+int(size) > len(data) {
			return 0, false
		}
		payload := data[offset+header : offset+int(size)]
		if boxType == "mvhd" || boxType == "mdhd" {
			if duration, ok := parseMP4TimeBox(payload); ok {
				return duration, true
			}
		}
		if isMP4ContainerBox(boxType) {
			if duration, ok := scanMP4Duration(payload, depth+1); ok {
				return duration, true
			}
		}
		offset += int(size)
	}
	return 0, false
}

func parseMP4TimeBox(payload []byte) (float64, bool) {
	if len(payload) < 20 {
		return 0, false
	}
	version := payload[0]
	if version == 1 {
		if len(payload) < 32 {
			return 0, false
		}
		timescale := binary.BigEndian.Uint32(payload[20:24])
		duration := binary.BigEndian.Uint64(payload[24:32])
		if timescale == 0 || duration == 0 {
			return 0, false
		}
		return float64(duration) / float64(timescale), true
	}
	timescale := binary.BigEndian.Uint32(payload[12:16])
	duration := binary.BigEndian.Uint32(payload[16:20])
	if timescale == 0 || duration == 0 {
		return 0, false
	}
	return float64(duration) / float64(timescale), true
}

func isMP4ContainerBox(boxType string) bool {
	switch boxType {
	case "moov", "trak", "mdia", "minf", "stbl", "edts":
		return true
	default:
		return false
	}
}

func countTextCharactersFromRequest(body []byte, contentType string) int {
	if len(body) == 0 {
		return 0
	}
	if count := countTextCharactersFromMultipart(body, contentType); count > 0 {
		return count
	}
	if !json.Valid(body) {
		return 0
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return 0
	}
	var texts []string
	collectBillableText(value, "", &texts)
	total := 0
	for _, text := range texts {
		total += utf8.RuneCountInString(text)
	}
	return total
}

func countTextCharactersFromMultipart(body []byte, contentType string) int {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		return 0
	}
	boundary := params["boundary"]
	if boundary == "" {
		return 0
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	total := 0
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return total
		}
		if err != nil {
			return total
		}
		name := strings.ToLower(strings.TrimSpace(part.FormName()))
		if name != "input" && name != "text" && name != "prompt" {
			continue
		}
		data, _ := io.ReadAll(part)
		text := strings.TrimSpace(string(data))
		if isBillableText(text) {
			total += utf8.RuneCountInString(text)
		}
	}
}

func collectBillableText(value any, key string, out *[]string) {
	switch v := value.(type) {
	case map[string]any:
		for childKey, child := range v {
			collectBillableText(child, childKey, out)
		}
	case []any:
		for _, child := range v {
			collectBillableText(child, key, out)
		}
	case string:
		if isBillableTextKey(key) && isBillableText(v) {
			*out = append(*out, strings.TrimSpace(v))
		}
	}
}

func isBillableTextKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "input", "text", "prompt", "content":
		return true
	default:
		return false
	}
}

func isBillableText(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || looksLikeURL(value) || strings.HasPrefix(strings.ToLower(value), "data:") {
		return false
	}
	if len(value) > 64 && looksLikeBase64(value) {
		return false
	}
	for _, r := range value {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r < 0x20 {
			return false
		}
	}
	return true
}

func looksLikeURL(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "s3://") ||
		strings.HasPrefix(lower, "gs://")
}

func looksLikeBase64(value string) bool {
	compact := compactBase64(value)
	if len(compact) < 64 || len(compact)%4 == 1 {
		return false
	}
	for _, r := range compact {
		if !isBase64AlphabetRune(r) {
			return false
		}
	}
	_, err := base64.StdEncoding.DecodeString(strings.NewReplacer("-", "+", "_", "/").Replace(compact))
	return err == nil
}

func isBase64AlphabetRune(r rune) bool {
	return r >= 'A' && r <= 'Z' ||
		r >= 'a' && r <= 'z' ||
		r >= '0' && r <= '9' ||
		r == '+' || r == '/' || r == '=' || r == '-' || r == '_'
}
