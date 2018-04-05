package payment

import (
	"fmt"
	"github.com/smartwalle/ngx"
	"github.com/smartwalle/paypal"
)

type PayPal struct {
	client              *paypal.PayPal
	ReturnURL           string // 支付成功之后回调 URL
	CancelURL           string // 用户取消付款回调 URL
	WebHookId           string
	ExperienceProfileId string
}

func NewPayPal(clientId, secret string, isProduction bool) *PayPal {
	var p = &PayPal{}
	p.client = paypal.New(clientId, secret, isProduction)
	return p
}

func (this *PayPal) Platform() string {
	return K_PLATFORM_PAYPAL
}

func (this *PayPal) CreatePayment(method string, payment *Payment) (url string, err error) {
	// PayPal 不用判断 method
	var p = &paypal.Payment{}
	p.Intent = paypal.K_PAYMENT_INTENT_SALE

	var cancelURL = ngx.MustURL(this.CancelURL)
	cancelURL.Add("order_no", payment.OrderNo)
	cancelURL.Add("platform", this.Platform())

	var returnURL = ngx.MustURL(this.ReturnURL)
	returnURL.Add("platform", this.Platform())
	returnURL.Add("order_no", payment.OrderNo)

	p.Payer = &paypal.Payer{}
	p.Payer.PaymentMethod = paypal.K_PAYMENT_METHOD_PAYPAL
	p.RedirectURLs = &paypal.RedirectURLs{}
	p.RedirectURLs.CancelURL = cancelURL.String()
	p.RedirectURLs.ReturnURL = returnURL.String()
	p.ExperienceProfileId = this.ExperienceProfileId

	var transaction = &paypal.Transaction{}
	transaction.InvoiceNumber = payment.OrderNo
	transaction.Amount = &paypal.Amount{}
	transaction.Amount.Currency = payment.Currency
	transaction.Amount.Details = &paypal.AmountDetails{}
	transaction.Amount.Details.HandlingFee = "0"
	transaction.Amount.Details.ShippingDiscount = "0"
	transaction.Amount.Details.Insurance = "0"

	if payment.ShippingAddress != nil {
		transaction.ItemList.ShippingAddress = &paypal.ShippingAddress{}
		transaction.ItemList.ShippingAddress.Line1 = payment.ShippingAddress.Line1
		transaction.ItemList.ShippingAddress.Line2 = payment.ShippingAddress.Line2
		transaction.ItemList.ShippingAddress.City = payment.ShippingAddress.City
		transaction.ItemList.ShippingAddress.State = payment.ShippingAddress.State
		transaction.ItemList.ShippingAddress.CountryCode = payment.ShippingAddress.CountryCode
		transaction.ItemList.ShippingAddress.PostalCode = payment.ShippingAddress.PostalCode
		transaction.ItemList.ShippingAddress.Phone = payment.ShippingAddress.Phone
	}

	var itemList = make([]*paypal.Item, 0, 0)
	var productAmount float64 = 0
	var productTax float64 = 0
	for _, p := range payment.ProductList {
		var item = &paypal.Item{}
		item.Name = p.Name
		item.Quantity = fmt.Sprintf("%d", p.Quantity)
		item.Price = fmt.Sprintf("%.2f", p.Price)
		item.Tax = fmt.Sprintf("%.2f", p.Tax)
		item.SKU = p.SKU
		item.Currency = payment.Currency
		itemList = append(itemList, item)

		productAmount += p.Price * float64(p.Quantity)
		productTax += p.Tax * float64(p.Quantity)
	}
	transaction.ItemList = &paypal.ItemList{Items: itemList}

	if payment.Shipping > 0 {
		transaction.Amount.Details.Shipping = fmt.Sprintf("%.2f", payment.Shipping)
	} else {
		transaction.Amount.Details.Shipping = "0.00"
	}
	if productTax > 0 {
		transaction.Amount.Details.Tax = fmt.Sprintf("%.2f", productTax)
	} else {
		transaction.Amount.Details.Tax = "0.00"
	}
	transaction.Amount.Details.Subtotal = fmt.Sprintf("%.2f", productAmount)

	var amount = productAmount + productTax + payment.Shipping
	transaction.Amount.Total = fmt.Sprintf("%.2f", amount)

	p.Transactions = []*paypal.Transaction{transaction}

	result, err := this.client.CreatePayment(p)
	if err != nil {
		return "", err
	}

	for _, link := range result.Links {
		if link.Rel == "approval_url" {
			return link.Href, nil
		}
	}
	return "", err
}

func (this *PayPal) TradeDetails(tradeNo string) (result *Trade, err error) {
	rsp, err := this.client.GetPaymentDetails(tradeNo)
	if err != nil {
		return nil, err
	}

	if rsp.State == paypal.K_PAYMENT_STATE_CREATED {
		if paymentRsp, err := this.client.ExecuteApprovedPayment(rsp.Id, rsp.Payer.PayerInfo.PayerId); err != nil {
			return nil, err
		} else {
			rsp = paymentRsp
		}
	}

	var trade = &Trade{}
	trade.Platform = this.Platform()
	trade.TradeNo = rsp.Id
	trade.TradeStatus = string(rsp.State)

	if len(rsp.Transactions) > 0 {
		var trans = rsp.Transactions[0]
		trade.OrderNo = trans.InvoiceNumber
		if trans.Amount != nil {
			trade.TotalAmount = trans.Amount.Total
		}
		if rsp.Payer != nil && rsp.Payer.PayerInfo != nil {
			trade.PayerId = rsp.Payer.PayerInfo.PayerId
			trade.PayerEmail = rsp.Payer.PayerInfo.Email
		}
		if len(trans.RelatedResources) > 0 {
			var relatedRes = trans.RelatedResources[0]
			trade.TradeStatus = string(relatedRes.Sale.State)
			if trade.TradeStatus == string(paypal.K_SALE_STATE_COMPLETED) {
				trade.TradeSuccess = true
			}
		}
	}
	return trade, nil
}