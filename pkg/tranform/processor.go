package transform

import (
	"errors"
	"fmt"
)

type PayloadProcessor struct {
	// Pipeline transforms: Applied 0..N for outgoing, N..0 for incoming.
	transforms []Transform
}

// NewPayloadProcessor creates a processor with a defined pipeline.
// Requires at least one transform. Use NewNoOpTransform() for an explicitly empty pipeline.
func NewPayloadProcessor(pipelineTransforms []Transform) (*PayloadProcessor, error) {
	if len(pipelineTransforms) == 0 {
		return nil, errors.New("payload processor requires at least one transform; use NewNoOpTransform() for an empty pipeline")
	}

	s := make([]Transform, len(pipelineTransforms))
	copy(s, pipelineTransforms)

	return &PayloadProcessor{
		transforms: s,
	}, nil
}

// PrepareOutput applies the pipeline transformations in forward order (0..N).
func (p *PayloadProcessor) PrepareOutput(payload []byte) ([]byte, error) {
	var err error
	currentPayload := payload
	// Iterate forward
	for i, transform := range p.transforms {
		currentPayload, err = transform.Apply(currentPayload)
		if err != nil {
			return nil, fmt.Errorf("prepare output: transform %d (%T) Apply failed: %w", i, transform, err)
		}
	}
	return currentPayload, nil
}

// ParseInput applies the pipeline transformations in reverse order (N..0).
func (p *PayloadProcessor) ParseInput(payload []byte) ([]byte, error) {
	var err error
	currentPayload := payload
	// Iterate backward
	for i := len(p.transforms) - 1; i >= 0; i-- {
		transform := p.transforms[i] // Get the transform at the current reverse index
		currentPayload, err = transform.Reverse(currentPayload)
		if err != nil {
			return nil, fmt.Errorf("parse input: transform %d (%T) Reverse failed: %w", i, transform, err)
		}
	}
	return currentPayload, nil
}
