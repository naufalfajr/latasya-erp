package account

import "github.com/naufal/latasya-erp/internal/model"

type Filter struct {
	Type     string
	IsActive *bool
	Search   string
	Limit    int
	Offset   int
}

type ListResult struct {
	Accounts []model.Account
	Total    int
}

type Draft struct {
	Code          string
	Name          string
	AccountType   string
	NormalBalance string
	Description   string
	IsActive      bool
	IsCash        bool
}
