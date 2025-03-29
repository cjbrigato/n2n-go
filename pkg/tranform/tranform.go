package transform

type Transform interface {
	Apply(data []byte) ([]byte, error)
	Reverse(data []byte) ([]byte, error)
}

type noOpTransform struct{}

func NewNoOpTransform() Transform                            { return &noOpTransform{} }
func (n *noOpTransform) Apply(data []byte) ([]byte, error)   { return data, nil }
func (n *noOpTransform) Reverse(data []byte) ([]byte, error) { return data, nil }
