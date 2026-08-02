package testutil

import (
	"context"
	"database/sql"

	"github.com/naufal/latasya-erp/internal/account"
	"github.com/naufal/latasya-erp/internal/company"
	"github.com/naufal/latasya-erp/internal/contact"
	"github.com/naufal/latasya-erp/internal/model"
)

type ContactFilter = contact.Filter

func CreateAccount(db *sql.DB, a *model.Account) error {
	created, err := account.New(db).Create(context.Background(), account.Actor{UserID: 1, CanManage: true}, account.Draft{Code: a.Code, Name: a.Name, AccountType: a.AccountType, NormalBalance: a.NormalBalance, Description: a.Description, IsActive: a.IsActive, IsCash: a.IsCash})
	if err == nil {
		*a = *created
	}
	return err
}

func ListContacts(db *sql.DB, f ContactFilter) ([]model.Contact, error) {
	result, err := contact.New(db).List(context.Background(), f)
	if err != nil {
		return nil, err
	}
	return result.Contacts, nil
}

func CreateContact(db *sql.DB, c *model.Contact) error {
	created, err := contact.New(db).Create(context.Background(), contact.Actor{UserID: 1, CanManage: true}, contact.Draft{Name: c.Name, ContactType: c.ContactType, Phone: c.Phone, Email: c.Email, Address: c.Address, Notes: c.Notes, MapsLink: c.MapsLink, Class: c.Class, DistanceKm: c.DistanceKm, HasSiblingDiscount: c.HasSiblingDiscount, IsReturnOnly: c.IsReturnOnly, RouteID: c.RouteID, IsActive: c.IsActive})
	if err == nil {
		*c = *created
	}
	return err
}

func GetCompanyProfile(db *sql.DB) (*model.CompanyProfile, error) {
	return company.New(db).Get(context.Background())
}

func GetOrCreatePortalCode(db *sql.DB, id int) (string, error) {
	return contact.New(db).EnsurePortalCode(context.Background(), contact.PortalIssuer{UserID: 1, CanIssue: true}, id)
}

func SetPortalCode(db *sql.DB, id int, code string) (string, error) {
	return contact.New(db).SetPortalCode(context.Background(), contact.Actor{UserID: 1, CanManage: true, CanManagePortal: true}, id, code)
}

func ContactsByPortalCode(db *sql.DB, code string) (*contact.PortalFamily, error) {
	return contact.New(db).FamilyByPortalCode(context.Background(), code)
}
