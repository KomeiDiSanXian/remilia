package dto

// ValidationReq ...
type ValidationReq struct {
	PlainToken string `json:"plain_token"`
	EventTs    string `json:"event_ts"`
}

// ValidationRsp ...
type ValidationRsp struct {
	PlainToken string `json:"plain_token"`
	Signature  string `json:"signature"`
}
