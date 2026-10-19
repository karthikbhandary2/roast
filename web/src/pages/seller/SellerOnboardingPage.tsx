import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../../api/client';
import { Navbar } from '../../components/Navbar';

export const SellerOnboardingPage: React.FC = () => {
  const navigate = useNavigate();

  const [businessName, setBusinessName] = useState('');
  const [provider, setProvider] = useState<'razorpay' | 'stripe'>('razorpay');
  const [payoutMethod, setPayoutMethod] = useState('bank_account');
  const [payoutAccountRef, setPayoutAccountRef] = useState('acc_12345');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [applied, setApplied] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setError(null);

    try {
      await api.onboardSeller({
        business_name: businessName,
        provider,
        payout_method: payoutMethod,
        payout_account_ref: payoutAccountRef,
      });

      setApplied(true);
    } catch (err: any) {
      setError(err.message || 'Onboarding submission failed');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div>
      <Navbar />

      <main className="container" style={{ padding: '32px 20px 60px', maxWidth: '540px' }}>
        <div style={{
          background: 'var(--surface-1)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-xl)',
          padding: '32px',
          boxShadow: 'var(--shadow-sm)',
        }}>
          <h1 style={{ fontSize: '20px', fontWeight: 700, marginBottom: '6px' }}>
            Become a Coffee Roaster on Roast co.
          </h1>
          <p style={{ fontSize: '13px', color: 'var(--text-secondary)', marginBottom: '24px' }}>
            List your specialty roasts and reach coffee lovers worldwide with instant order routing.
          </p>

          {applied ? (
            <div style={{
              background: 'var(--badge-warning-bg)',
              border: '1px solid #fae2c8',
              borderRadius: 'var(--radius)',
              padding: '20px',
              textAlign: 'center',
            }}>
              <div style={{ fontSize: '32px', marginBottom: '8px' }}>⏳</div>
              <h3 style={{ fontSize: '15px', fontWeight: 700, marginBottom: '4px' }}>
                Application Under Review
              </h3>
              <p style={{ fontSize: '13px', color: 'var(--text-secondary)', marginBottom: '16px' }}>
                Your roaster profile has been submitted and is currently being verified by our platform admins.
              </p>
              <button
                type="button"
                onClick={() => navigate('/')}
                className="btn-secondary"
              >
                Back to Home
              </button>
            </div>
          ) : (
            <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
              {error && (
                <div style={{
                  background: 'var(--badge-danger-bg)',
                  color: 'var(--text-danger)',
                  padding: '10px 14px',
                  borderRadius: 'var(--radius)',
                  fontSize: '13px',
                }}>
                  {error}
                </div>
              )}

              <div>
                <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '4px' }}>
                  Roastery / Business Name *
                </label>
                <input
                  type="text"
                  required
                  value={businessName}
                  onChange={(e) => setBusinessName(e.target.value)}
                  placeholder="e.g. Blue Tokai Roasters"
                  className="input-control"
                />
              </div>

              <div>
                <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '4px' }}>
                  Primary Payment Provider *
                </label>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
                  <button
                    type="button"
                    onClick={() => setProvider('phonepe' as any)}
                    style={{
                      padding: '12px',
                      borderRadius: 'var(--radius)',
                      border: `2px solid ${provider === ('phonepe' as any) || provider === 'razorpay' ? 'var(--fill-accent)' : 'var(--border)'}`,
                      background: provider === ('phonepe' as any) || provider === 'razorpay' ? 'var(--badge-warning-bg)' : 'var(--surface-2)',
                      textAlign: 'left',
                      cursor: 'pointer',
                    }}
                  >
                    <div style={{ fontSize: '13px', fontWeight: 600 }}>PhonePe (UPI / INR)</div>
                    <div style={{ fontSize: '11px', color: 'var(--text-muted)' }}>UPI, PhonePe QR & Direct Pay</div>
                  </button>

                  <button
                    type="button"
                    onClick={() => setProvider('stripe')}
                    style={{
                      padding: '12px',
                      borderRadius: 'var(--radius)',
                      border: `2px solid ${provider === 'stripe' ? 'var(--fill-accent)' : 'var(--border)'}`,
                      background: provider === 'stripe' ? 'var(--badge-warning-bg)' : 'var(--surface-2)',
                      textAlign: 'left',
                      cursor: 'pointer',
                    }}
                  >
                    <div style={{ fontSize: '13px', fontWeight: 600 }}>Stripe Connect</div>
                    <div style={{ fontSize: '11px', color: 'var(--text-muted)' }}>USD, GBP, JPY Cards</div>
                  </button>
                </div>
              </div>

              <div>
                <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '4px' }}>
                  Payout Method *
                </label>
                <select
                  value={payoutMethod}
                  onChange={(e) => setPayoutMethod(e.target.value)}
                  className="input-control"
                >
                  <option value="bank_account">Bank Account Transfer</option>
                  <option value="upi">UPI Direct Transfer</option>
                  <option value="ach">ACH Transfer</option>
                </select>
              </div>

              <div>
                <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '4px' }}>
                  Payout Account Reference *
                </label>
                <input
                  type="text"
                  required
                  value={payoutAccountRef}
                  onChange={(e) => setPayoutAccountRef(e.target.value)}
                  placeholder="e.g. acc_12345 or vpa@upi"
                  className="input-control"
                />
                <span style={{ fontSize: '11px', color: 'var(--text-muted)', marginTop: '4px', display: 'block' }}>
                  Opaque token or virtual identifier with payment provider.
                </span>
              </div>

              <button
                type="submit"
                disabled={submitting}
                className="btn-accent"
                style={{ width: '100%', height: '44px', fontWeight: 600, marginTop: '8px' }}
              >
                {submitting ? 'Submitting Application...' : 'Submit Roaster Application'}
              </button>
            </form>
          )}
        </div>
      </main>
    </div>
  );
};
