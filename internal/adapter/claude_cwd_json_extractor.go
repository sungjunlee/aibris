package adapter

func (e *cwdMetadataExtractor) feed(data []byte) string {
	var foundCWD string
	for i := 0; i < len(data); {
		b := data[i]
		if !isJSONWhitespace(b) {
			e.recordContent = true
		}
		if e.invalid {
			i++
			continue
		}
		if e.inString {
			if cwd := e.feedStringByte(b); cwd != "" {
				if foundCWD == "" {
					foundCWD = cwd
				}
			}
			i++
			continue
		}
		if e.literalRemaining != "" {
			if b != e.literalRemaining[0] {
				e.invalid = true
			} else {
				e.literalRemaining = e.literalRemaining[1:]
				if e.literalRemaining == "" {
					e.finishValue()
				}
			}
			i++
			continue
		}
		if e.numberState != jsonNumberNone {
			if e.feedNumberByte(b) {
				i++
				continue
			}
			if !e.numberCanEnd() || !isJSONValueDelimiter(b) {
				e.invalid = true
				i++
				continue
			}
			e.numberState = jsonNumberNone
			e.finishValue()
			continue
		}
		if e.complete {
			if !isJSONWhitespace(b) {
				e.invalid = true
			}
			i++
			continue
		}
		if !e.started {
			if isJSONWhitespace(b) {
				i++
				continue
			}
			if b != '{' {
				e.invalid = true
				i++
				continue
			}
			e.started = true
			e.containers = append(e.containers, jsonContainer{
				kind:  '{',
				state: jsonObjectKeyOrEnd,
			})
			i++
			continue
		}
		if isJSONWhitespace(b) {
			i++
			continue
		}
		if len(e.containers) == 0 {
			e.invalid = true
			i++
			continue
		}

		frame := &e.containers[len(e.containers)-1]
		switch frame.state {
		case jsonObjectKeyOrEnd:
			if b == '}' {
				e.closeContainer('}')
			} else if b == '"' {
				e.startKeyString()
			} else {
				e.invalid = true
			}
		case jsonObjectKey:
			if b == '"' {
				e.startKeyString()
			} else {
				e.invalid = true
			}
		case jsonObjectColon:
			if b == ':' {
				frame.state = jsonObjectValue
			} else {
				e.invalid = true
			}
		case jsonObjectValue, jsonArrayValueOrEnd, jsonArrayValue:
			if frame.state == jsonArrayValueOrEnd && b == ']' {
				e.closeContainer(']')
			} else {
				e.startValue(b)
			}
		case jsonObjectCommaOrEnd:
			if b == ',' {
				frame.state = jsonObjectKey
			} else if b == '}' {
				e.closeContainer('}')
			} else {
				e.invalid = true
			}
		case jsonArrayCommaOrEnd:
			if b == ',' {
				frame.state = jsonArrayValue
			} else if b == ']' {
				e.closeContainer(']')
			} else {
				e.invalid = true
			}
		}
		i++
	}
	return foundCWD
}
