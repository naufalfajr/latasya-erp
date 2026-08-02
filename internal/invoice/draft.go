package invoice

import (
	"fmt"
	"strings"
)

type DraftLine struct {
	Description string
	Quantity    int
	UnitPrice   int
	AccountID   int
}

type Draft struct {
	ContactID   int
	InvoiceDate string
	DueDate     string
	TaxAmount   int
	Notes       string
	Lines       []DraftLine
}

func validateDraft(draft Draft) error {
	fields := map[string]string{}
	if draft.ContactID <= 0 {
		fields["contact_id"] = "Customer is required"
	}
	if strings.TrimSpace(draft.InvoiceDate) == "" {
		fields["invoice_date"] = "Invoice date is required"
	}
	if strings.TrimSpace(draft.DueDate) == "" {
		fields["due_date"] = "Due date is required"
	}
	if draft.TaxAmount < 0 {
		fields["tax_amount"] = "Tax amount must not be negative"
	}
	if len(draft.Lines) == 0 {
		fields["lines"] = "At least one line item is required"
	}
	for i, line := range draft.Lines {
		prefix := fmt.Sprintf("lines[%d]", i)
		if strings.TrimSpace(line.Description) == "" {
			fields[prefix+".description"] = "Description required"
		}
		if line.Quantity <= 0 {
			fields[prefix+".quantity"] = "Quantity must be positive"
		}
		if line.UnitPrice <= 0 {
			fields[prefix+".unit_price"] = "Price required"
		}
		if line.AccountID <= 0 {
			fields[prefix+".account_id"] = "Account required"
		}
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// ValidateDraft exposes the same validation used by mutations so adapters can
// present all field errors before invoking a write.
func ValidateDraft(draft Draft) error {
	return validateDraft(draft)
}
