package netstruct

type RegisterRequest struct {
	EdgeMACAddr   string
	EdgeDesc      string
	CommunityName string
}

type RetryRegisterRequest struct{}
