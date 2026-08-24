package cmd

import (
	"github.com/spf13/cobra"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/avactl/output"
	"github.com/silverbp/ava/internal/avactl/resource"
)

func newCreateContactCmd() *cobra.Command {
	var contactNumber, name, email, phone string
	var ledgerAccount, paymentTerms int32
	var isCustomer, isVendor bool
	var creditLimit string

	cmd := &cobra.Command{
		Use:   "contact",
		Short: "Create a customer/vendor contact",
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.CreateContactRequest{
				BusinessId:    businessID,
				ContactNumber: contactNumber,
				Name:          name,
				IsCustomer:    isCustomer,
				IsVendor:      isVendor,
			}
			if email != "" {
				req.Email = &email
			}
			if phone != "" {
				req.Phone = &phone
			}
			if ledgerAccount != 0 {
				req.LedgerAccountId = &ledgerAccount
			}
			if paymentTerms != 0 {
				req.PaymentTermsDays = &paymentTerms
			}
			if creditLimit != "" {
				req.CreditLimit = &avav1.Decimal{Value: creditLimit}
			}

			resp, err := avav1.NewContactServiceClient(conn).CreateContact(cmd.Context(), req)
			if err != nil {
				return err
			}
			res, _ := resource.Lookup("contact")
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetContact(), res.Columns)
		},
	}
	cmd.Flags().StringVar(&contactNumber, "contact-number", "", "unique contact number (required)")
	cmd.Flags().StringVar(&name, "name", "", "contact name (required)")
	cmd.Flags().StringVar(&email, "email", "", "email address")
	cmd.Flags().StringVar(&phone, "phone", "", "phone number")
	cmd.Flags().BoolVar(&isCustomer, "customer", true, "this contact is a customer")
	cmd.Flags().BoolVar(&isVendor, "vendor", false, "this contact is a vendor")
	cmd.Flags().Int32Var(&ledgerAccount, "ledger-account", 0, "this contact's AR/AP ledger account id, for posting invoices/payments")
	cmd.Flags().Int32Var(&paymentTerms, "payment-terms", 0, "default payment terms, in days")
	cmd.Flags().StringVar(&creditLimit, "credit-limit", "", "credit limit")
	_ = cmd.MarkFlagRequired("contact-number")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newCreateServiceCmd() *cobra.Command {
	var code, name, description, unit, price, cost, defaultTaxRate string
	var taxable bool

	cmd := &cobra.Command{
		Use:   "service",
		Short: "Create a catalog service/product",
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &avav1.CreateServiceRequest{
				BusinessId:  businessID,
				ServiceCode: code,
				Name:        name,
				IsTaxable:   taxable,
				RetailPrice: &avav1.Decimal{Value: price},
			}
			if description != "" {
				req.Description = &description
			}
			if unit != "" {
				req.UnitOfMeasure = &unit
			}
			if cost != "" {
				req.CostPrice = &avav1.Decimal{Value: cost}
			}
			if defaultTaxRate != "" {
				req.DefaultTaxRate = &avav1.Decimal{Value: defaultTaxRate}
			}

			resp, err := avav1.NewServiceCatalogServiceClient(conn).CreateService(cmd.Context(), req)
			if err != nil {
				return err
			}
			res, _ := resource.Lookup("service")
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetService(), res.Columns)
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "service code (required)")
	cmd.Flags().StringVar(&name, "name", "", "service name (required)")
	cmd.Flags().StringVar(&description, "description", "", "service description")
	cmd.Flags().StringVar(&unit, "unit", "", "unit of measure, e.g. HOUR, EACH (default EACH)")
	cmd.Flags().StringVar(&price, "price", "", "retail price (required)")
	cmd.Flags().StringVar(&cost, "cost", "", "cost price")
	cmd.Flags().BoolVar(&taxable, "taxable", false, "taxable by default")
	cmd.Flags().StringVar(&defaultTaxRate, "default-tax-rate", "", "default tax rate, e.g. 0.0825")
	_ = cmd.MarkFlagRequired("code")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("price")
	return cmd
}

func newCreateTaxRateCmd() *cobra.Command {
	var name, rate string
	var liabilityAccount int32

	cmd := &cobra.Command{
		Use:   "tax-rate",
		Short: "Create a named tax rate",
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, _, businessID, err := dial()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := avav1.NewTaxRateServiceClient(conn).CreateTaxRate(cmd.Context(), &avav1.CreateTaxRateRequest{
				BusinessId:            businessID,
				Name:                  name,
				Rate:                  &avav1.Decimal{Value: rate},
				TaxLiabilityAccountId: liabilityAccount,
			})
			if err != nil {
				return err
			}
			res, _ := resource.Lookup("tax-rate")
			return output.PrintOne(cmd.OutOrStdout(), flagOutput, resp.GetTaxRate(), res.Columns)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "tax rate name, e.g. \"Sales Tax\" (required)")
	cmd.Flags().StringVar(&rate, "rate", "", "rate as a decimal fraction, e.g. 0.0825 for 8.25% (required)")
	cmd.Flags().Int32Var(&liabilityAccount, "liability-account", 0, "TAX_LIABILITY ledger account id this rate is collected into (required)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("rate")
	_ = cmd.MarkFlagRequired("liability-account")
	return cmd
}
