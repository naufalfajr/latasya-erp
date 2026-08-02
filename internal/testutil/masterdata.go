package testutil

import (
	"context"
	"database/sql"

	"github.com/naufal/latasya-erp/internal/account"
	"github.com/naufal/latasya-erp/internal/company"
	"github.com/naufal/latasya-erp/internal/contact"
	"github.com/naufal/latasya-erp/internal/model"
)

type AccountFilter = account.Filter
type ContactFilter = contact.Filter

var ErrPortalCodeTaken = contact.ErrPortalCodeTaken

func ListAccounts(db *sql.DB, f AccountFilter) ([]model.Account, error) {
	result, err := account.New(db).List(context.Background(), f)
	if err != nil {
		return nil, err
	}
	return result.Accounts, nil
}

func GetAccount(db *sql.DB, id int) (*model.Account, error) {
	return account.New(db).Get(context.Background(), id)
}

func CreateAccount(db *sql.DB, a *model.Account) error {
	created, err := account.New(db).Create(context.Background(), account.Actor{UserID: 1, CanManage: true}, account.Draft{Code: a.Code, Name: a.Name, AccountType: a.AccountType, NormalBalance: a.NormalBalance, Description: a.Description, IsActive: a.IsActive, IsCash: a.IsCash})
	if err == nil {
		*a = *created
	}
	return err
}

func UpdateAccount(db *sql.DB, a *model.Account) error {
	updated, err := account.New(db).Update(context.Background(), account.Actor{UserID: 1, CanManage: true}, a.ID, account.Draft{Code: a.Code, Name: a.Name, AccountType: a.AccountType, NormalBalance: a.NormalBalance, Description: a.Description, IsActive: a.IsActive, IsCash: a.IsCash})
	if err == nil {
		*a = *updated
	}
	return err
}

func DeleteAccount(db *sql.DB, id int) error {
	_, err := account.New(db).Delete(context.Background(), account.Actor{UserID: 1, CanManage: true}, id)
	return err
}

func ListContacts(db *sql.DB, f ContactFilter) ([]model.Contact, error) {
	result, err := contact.New(db).List(context.Background(), f)
	if err != nil {
		return nil, err
	}
	return result.Contacts, nil
}

func GetContact(db *sql.DB, id int) (*model.Contact, error) {
	return contact.New(db).Get(context.Background(), id)
}

func CreateContact(db *sql.DB, c *model.Contact) error {
	created, err := contact.New(db).Create(context.Background(), contact.Actor{UserID: 1, CanManage: true}, contact.Draft{Name: c.Name, ContactType: c.ContactType, Phone: c.Phone, Email: c.Email, Address: c.Address, Notes: c.Notes, MapsLink: c.MapsLink, Class: c.Class, DistanceKm: c.DistanceKm, HasSiblingDiscount: c.HasSiblingDiscount, IsReturnOnly: c.IsReturnOnly, RouteID: c.RouteID, IsActive: c.IsActive})
	if err == nil {
		*c = *created
	}
	return err
}

func UpdateContact(db *sql.DB, c *model.Contact) error {
	updated, err := contact.New(db).Update(context.Background(), contact.Actor{UserID: 1, CanManage: true}, c.ID, contact.Draft{Name: c.Name, ContactType: c.ContactType, Phone: c.Phone, Email: c.Email, Address: c.Address, Notes: c.Notes, MapsLink: c.MapsLink, Class: c.Class, DistanceKm: c.DistanceKm, HasSiblingDiscount: c.HasSiblingDiscount, IsReturnOnly: c.IsReturnOnly, RouteID: c.RouteID, IsActive: c.IsActive})
	if err == nil {
		*c = *updated
	}
	return err
}

func DeleteContact(db *sql.DB, id int) error {
	_, err := contact.New(db).Delete(context.Background(), contact.Actor{UserID: 1, CanManage: true}, id)
	return err
}

func GetCompanyProfile(db *sql.DB) (*model.CompanyProfile, error) {
	return company.New(db).Get(context.Background())
}

func UpdateCompanyProfile(db *sql.DB, profile *model.CompanyProfile) error {
	updated, err := company.New(db).Update(context.Background(), company.Actor{UserID: 1, CanManage: true}, *profile)
	if err == nil {
		*profile = *updated
	}
	return err
}

func GetOrCreatePortalCode(db *sql.DB, id int) (string, error) {
	return contact.New(db).GetOrCreatePortalCode(context.Background(), id)
}

func RegeneratePortalCode(db *sql.DB, id int) (string, error) {
	return contact.New(db).SetPortalCode(context.Background(), contact.Actor{UserID: 1, CanManage: true, CanManagePortal: true}, id, "")
}

func SetPortalCode(db *sql.DB, id int, code string) (string, error) {
	return contact.New(db).SetPortalCode(context.Background(), contact.Actor{UserID: 1, CanManage: true, CanManagePortal: true}, id, code)
}

func ContactsByPortalCode(db *sql.DB, code string) (*contact.PortalFamily, error) {
	return contact.New(db).FamilyByPortalCode(context.Background(), code)
}

func NormalizePortalCode(code string) string { return contact.NormalizePortalCode(code) }
