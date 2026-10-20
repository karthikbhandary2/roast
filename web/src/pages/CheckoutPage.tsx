import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { api, formatMoney } from '../api/client';
import { Cart, ShippingAddress } from '../types';
import { Navbar } from '../components/Navbar';
import { useAuth } from '../hooks/useAuth';

export const CheckoutPage: React.FC = () => {
  const { user } = useAuth();
  const navigate = useNavigate();

  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [cart, setCart] = useState<Cart | null>(null);
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Step 1: Address
  const [address, setAddress] = useState<ShippingAddress>({
    full_name: user?.display_name || 'Jane Buyer',
    line1: '123 Specialty Lane',
    line2: 'Apt 4B',
    city: 'Bengaluru',
    postal_code: '560001',
    country_code: 'IN',
    phone: '+919876543210',
  });

  // Step 2: Payment
  const [paymentMethod, setPaymentMethod] = useState<'card' | 'upi'>('card');
  const [upiId, setUpiId] = useState('buyer@okhdfcbank');
  const [cardNumber, setCardNumber] = useState('•••• •••• •••• 4242');

  useEffect(() => {
    if (!user) {
      navigate('/login');
      return;
    }
    api.getCart()
      .then((c) => {
        if (!c?.items || c.items.length === 0) {
          navigate('/cart');
        } else {
          setCart(c);
        }
      })
      .finally(() => setLoading(false));
  }, [user]);

  const handlePlaceOrder = async () => {
    setSubmitting(true);
    setError(null);

    const idempotencyKey = crypto.randomUUID ? crypto.randomUUID() : `key-${Date.now()}`;

    try {
      const order = await api.checkout({
        shipping_address: address,
        payment_method: paymentMethod,
        payment_method_token: paymentMethod === 'upi' ? upiId : 'tok_mock_success',
      }, idempotencyKey);

      const subOrder = order.sub_orders?.[0];

      // If paying via UPI / PhonePe, initiate PhonePe and redirect to PhonePe official payment page
      if (paymentMethod === 'upi') {
        try {
          const ppeRes = await api.initiatePhonePe({
            amount: subtotalMinor,
            order_id: order.id,
            sub_order_id: subOrder?.id || '',
          });

          if (ppeRes?.redirect_url) {
            window.location.href = ppeRes.redirect_url;
            return;
          }
        } catch (ppeErr) {
          console.warn('PhonePe initiation error, falling back to confirmation page:', ppeErr);
        }
      }

      // Cart has been emptied upon checkout
      window.dispatchEvent(new CustomEvent('cart-updated', { detail: { count: 0 } }));

      navigate(`/orders/${order.id}/confirmation`, {
        state: { vpa: upiId, paymentMethod },
      });
    } catch (err: any) {
      setError(err.message || 'Checkout failed. Please retry.');
      setSubmitting(false);
    }
  };

  if (loading) {
    return (
      <div>
        <Navbar />
        <div style={{ textAlign: 'center', padding: '60px 0', color: 'var(--text-muted)' }}>
          <i className="ti ti-loader-2 ti-spin" style={{ fontSize: '24px' }} />
        </div>
      </div>
    );
  }

  const items = cart?.items || [];
  const subtotalMinor = items.reduce((acc, item) => {
    const minor = Number(item.unit_price?.amount_minor || 0);
    return acc + minor * item.quantity;
  }, 0);
  const currency = items[0]?.unit_price?.currency_code || 'USD';

  return (
    <div>
      <Navbar />

      <main className="container checkout-container-responsive" style={{ padding: '24px 20px 60px', maxWidth: '840px' }}>
        {/* Step indicator matching checkout_address_wireframe.html */}
        <div style={{
          display: 'flex',
          justifyContent: 'center',
          alignItems: 'center',
          gap: '16px',
          marginBottom: '32px',
          flexWrap: 'wrap',
        }}>
          {[
            { num: 1, label: 'Address' },
            { num: 2, label: 'Payment' },
            { num: 3, label: 'Review' },
          ].map((s, idx) => (
            <React.Fragment key={s.num}>
              <div style={{
                display: 'flex',
                alignItems: 'center',
                gap: '8px',
                fontWeight: step === s.num ? 700 : 500,
                color: step >= s.num ? 'var(--text-primary)' : 'var(--text-muted)',
              }}>
                <span style={{
                  width: '26px',
                  height: '26px',
                  borderRadius: '50%',
                  background: step >= s.num ? 'var(--fill-accent)' : 'var(--border-strong)',
                  color: '#fff',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  fontSize: '12px',
                }}>
                  {step > s.num ? '✓' : s.num}
                </span>
                <span style={{ fontSize: '13px' }}>{s.label}</span>
              </div>
              {idx < 2 && (
                <div style={{
                  width: '32px',
                  height: '1px',
                  background: step > s.num ? 'var(--fill-accent)' : 'var(--border-strong)',
                }} />
              )}
            </React.Fragment>
          ))}
        </div>

        {error && (
          <div style={{
            background: 'var(--badge-danger-bg)',
            border: '1px solid #fad2d2',
            color: 'var(--text-danger)',
            borderRadius: 'var(--radius)',
            padding: '12px 16px',
            marginBottom: '20px',
            fontSize: '13px',
          }}>
            {error}
          </div>
        )}

        {/* Step 1: Address Form */}
        {step === 1 && (
          <div style={{
            background: 'var(--surface-1)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-xl)',
            padding: '32px',
          }}>
            <h2 style={{ fontSize: '18px', fontWeight: 700, marginBottom: '20px' }}>Shipping Address</h2>

            <div className="checkout-2col-responsive" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px', marginBottom: '16px' }}>
              <div>
                <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '6px' }}>
                  Full Name *
                </label>
                <input
                  type="text"
                  required
                  value={address.full_name}
                  onChange={(e) => setAddress({ ...address, full_name: e.target.value })}
                  className="input-control"
                />
              </div>
              <div>
                <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '6px' }}>
                  Phone Number *
                </label>
                <input
                  type="text"
                  required
                  value={address.phone}
                  onChange={(e) => setAddress({ ...address, phone: e.target.value })}
                  className="input-control"
                />
              </div>
            </div>

            <div style={{ marginBottom: '16px' }}>
              <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '6px' }}>
                Address Line 1 *
              </label>
              <input
                type="text"
                required
                value={address.line1}
                onChange={(e) => setAddress({ ...address, line1: e.target.value })}
                className="input-control"
              />
            </div>

            <div className="checkout-3col-responsive" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: '16px', marginBottom: '24px' }}>
              <div>
                <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '6px' }}>
                  City *
                </label>
                <input
                  type="text"
                  required
                  value={address.city}
                  onChange={(e) => setAddress({ ...address, city: e.target.value })}
                  className="input-control"
                />
              </div>
              <div>
                <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '6px' }}>
                  Postal Code *
                </label>
                <input
                  type="text"
                  required
                  value={address.postal_code}
                  onChange={(e) => setAddress({ ...address, postal_code: e.target.value })}
                  className="input-control"
                />
              </div>
              <div>
                <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '6px' }}>
                  Country *
                </label>
                <input
                  type="text"
                  required
                  value={address.country_code}
                  onChange={(e) => setAddress({ ...address, country_code: e.target.value })}
                  className="input-control"
                />
              </div>
            </div>

            <button
              type="button"
              onClick={() => {
                if (address.full_name && address.line1 && address.city) {
                  setStep(2);
                } else {
                  alert('Please fill in all required address fields.');
                }
              }}
              className="btn-accent"
              style={{ width: '100%', height: '44px', fontWeight: 600 }}
            >
              Continue to Payment →
            </button>
          </div>
        )}

        {/* Step 2: Payment details matching checkout_payment_wireframe.html */}
        {step === 2 && (
          <div style={{
            background: 'var(--surface-1)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-xl)',
            padding: '32px',
          }}>
            <h2 style={{ fontSize: '18px', fontWeight: 700, marginBottom: '20px' }}>Payment Method</h2>

            <div style={{ display: 'flex', gap: '16px', marginBottom: '24px' }}>
              <button
                type="button"
                onClick={() => setPaymentMethod('card')}
                style={{
                  flex: 1,
                  padding: '16px',
                  borderRadius: 'var(--radius)',
                  border: `2px solid ${paymentMethod === 'card' ? 'var(--fill-accent)' : 'var(--border)'}`,
                  background: paymentMethod === 'card' ? 'var(--badge-warning-bg)' : 'var(--surface-2)',
                  textAlign: 'left',
                }}
              >
                <div style={{ fontWeight: 600, fontSize: '14px', marginBottom: '4px' }}>💳 Credit / Debit Card</div>
                <div style={{ fontSize: '12px', color: 'var(--text-muted)' }}>Visa, Mastercard, Amex</div>
              </button>

              <button
                type="button"
                onClick={() => setPaymentMethod('upi')}
                style={{
                  flex: 1,
                  padding: '16px',
                  borderRadius: 'var(--radius)',
                  border: `2px solid ${paymentMethod === 'upi' ? 'var(--fill-accent)' : 'var(--border)'}`,
                  background: paymentMethod === 'upi' ? 'var(--badge-warning-bg)' : 'var(--surface-2)',
                  textAlign: 'left',
                }}
              >
                <div style={{ fontWeight: 600, fontSize: '14px', marginBottom: '4px' }}>⚡ PhonePe UPI</div>
                <div style={{ fontSize: '12px', color: 'var(--text-muted)' }}>UPI QR, Intent & Apps</div>
              </button>
            </div>

            {paymentMethod === 'card' ? (
              <div style={{ marginBottom: '24px' }}>
                <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '6px' }}>
                  Card Details (Mock Test Mode)
                </label>
                <input
                  type="text"
                  value={cardNumber}
                  onChange={(e) => setCardNumber(e.target.value)}
                  className="input-control"
                  style={{ marginBottom: '12px' }}
                />
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px' }}>
                  <input type="text" placeholder="MM / YY" defaultValue="12/28" className="input-control" />
                  <input type="text" placeholder="CVC" defaultValue="123" className="input-control" />
                </div>
              </div>
            ) : (
              <div style={{ marginBottom: '24px' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' }}>
                  <label style={{ fontSize: '12px', fontWeight: 600 }}>
                    Virtual Payment Address (VPA / UPI ID)
                  </label>
                  {upiId.includes('@') && upiId.length > 5 ? (
                    <span style={{ fontSize: '11px', color: 'var(--text-success)', fontWeight: 600 }}>
                      ✓ Valid UPI ID format
                    </span>
                  ) : (
                    <span style={{ fontSize: '11px', color: 'var(--text-muted)' }}>
                      Enter your handle e.g. name@bank
                    </span>
                  )}
                </div>

                <input
                  type="text"
                  value={upiId}
                  onChange={(e) => setUpiId(e.target.value)}
                  placeholder="e.g. yourname@okhdfcbank"
                  className="input-control"
                  style={{ marginBottom: '10px' }}
                />

                {/* Popular UPI handle pills */}
                <div style={{ display: 'flex', gap: '6px', flexWrap: 'wrap', marginBottom: '12px' }}>
                  <span style={{ fontSize: '11px', color: 'var(--text-muted)', alignSelf: 'center' }}>
                    Popular:
                  </span>
                  {['@okhdfcbank', '@okaxis', '@oksbi', '@ybl', '@paytm', '@ibl'].map((handle) => (
                    <button
                      key={handle}
                      type="button"
                      onClick={() => {
                        const prefix = upiId.includes('@') ? upiId.split('@')[0] : upiId || 'buyer';
                        setUpiId(`${prefix}${handle}`);
                      }}
                      style={{
                        padding: '3px 8px',
                        background: 'var(--surface-2)',
                        border: '1px solid var(--border)',
                        borderRadius: '12px',
                        fontSize: '11px',
                        color: 'var(--text-secondary)',
                        cursor: 'pointer',
                      }}
                    >
                      {handle}
                    </button>
                  ))}
                </div>

                <div style={{
                  background: 'var(--badge-warning-bg)',
                  border: '1px solid #fae7c9',
                  borderRadius: 'var(--radius)',
                  padding: '10px 14px',
                  fontSize: '12px',
                  color: 'var(--text-primary)',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '8px',
                }}>
                  <i className="ti ti-qrcode" style={{ fontSize: '16px', color: 'var(--fill-accent)' }} />
                  <span>
                    Prefer scanning a QR? A dynamic UPI QR code will also be available on the next step.
                  </span>
                </div>
              </div>
            )}

            <div style={{ display: 'flex', gap: '12px' }}>
              <button
                type="button"
                onClick={() => setStep(1)}
                className="btn-secondary"
                style={{ flex: 1, height: '44px' }}
              >
                ← Back
              </button>
              <button
                type="button"
                onClick={() => setStep(3)}
                className="btn-accent"
                style={{ flex: 2, height: '44px', fontWeight: 600 }}
              >
                Review Order →
              </button>
            </div>
          </div>
        )}

        {/* Step 3: Review & Place Order matching checkout_review_wireframe.html */}
        {step === 3 && (
          <div style={{
            background: 'var(--surface-1)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-xl)',
            padding: '32px',
          }}>
            <h2 style={{ fontSize: '18px', fontWeight: 700, marginBottom: '20px' }}>Review & Confirm</h2>

            <div style={{
              display: 'grid',
              gridTemplateColumns: '1fr 1fr',
              gap: '20px',
              padding: '16px',
              borderRadius: 'var(--radius)',
              background: 'var(--surface-2)',
              marginBottom: '24px',
              fontSize: '13px',
            }}>
              <div>
                <strong>Shipping to:</strong>
                <div style={{ color: 'var(--text-secondary)', marginTop: '4px' }}>
                  {address.full_name}<br />
                  {address.line1}, {address.city}<br />
                  {address.postal_code}, {address.country_code}<br />
                  {address.phone}
                </div>
              </div>
              <div>
                <strong>Payment:</strong>
                <div style={{ color: 'var(--text-secondary)', marginTop: '4px' }}>
                  {paymentMethod === 'card' ? '💳 Credit/Debit Card (•••• 4242)' : `⚡ UPI (${upiId})`}
                </div>
              </div>
            </div>

            <h3 style={{ fontSize: '14px', fontWeight: 600, marginBottom: '12px' }}>Items Summary</h3>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '12px', marginBottom: '24px' }}>
              {items.map((it) => (
                <div key={it.product_id} style={{ display: 'flex', justifyContent: 'space-between', fontSize: '13px' }}>
                  <span>{it.quantity}x Specialty Roast</span>
                  <span style={{ fontWeight: 600 }}>
                    {formatMoney({ currency_code: currency, amount_minor: Number(it.unit_price.amount_minor) * it.quantity })}
                  </span>
                </div>
              ))}
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '13px' }}>
                <span>Shipping</span>
                <span style={{ color: 'var(--text-success)', fontWeight: 600 }}>FREE</span>
              </div>
              <div style={{
                display: 'flex',
                justifyContent: 'space-between',
                fontSize: '16px',
                fontWeight: 700,
                paddingTop: '12px',
                borderTop: '1px solid var(--border)',
              }}>
                <span>Total Due</span>
                <span>{formatMoney({ currency_code: currency, amount_minor: subtotalMinor })}</span>
              </div>
            </div>

            <div style={{ display: 'flex', gap: '12px' }}>
              <button
                type="button"
                onClick={() => setStep(2)}
                disabled={submitting}
                className="btn-secondary"
                style={{ flex: 1, height: '44px' }}
              >
                ← Back
              </button>
              <button
                type="button"
                onClick={handlePlaceOrder}
                disabled={submitting}
                className="btn-accent"
                style={{ flex: 2, height: '44px', fontWeight: 600 }}
              >
                {submitting ? (
                  <><i className="ti ti-loader-2 ti-spin" /> Orchestrating Saga...</>
                ) : (
                  <>Place Order ({formatMoney({ currency_code: currency, amount_minor: subtotalMinor })})</>
                )}
              </button>
            </div>
          </div>
        )}
      </main>
    </div>
  );
};
