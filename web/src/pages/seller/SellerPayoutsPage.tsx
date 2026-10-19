import React, { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { api, formatMoney } from '../../api/client';
import { Navbar } from '../../components/Navbar';

export const SellerPayoutsPage: React.FC = () => {
  const [data, setData] = useState<any>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    Promise.all([api.getSellerDashboard(), api.getSellerPayouts().catch(() => ({ payouts: [] }))])
      .then(([dash, payouts]) => {
        setData({
          ...dash,
          payouts: payouts?.payouts || [],
        });
      })
      .finally(() => setLoading(false));
  }, []);

  return (
    <div>
      <Navbar />

      <main className="container" style={{ padding: '24px 20px 60px', maxWidth: '840px' }}>
        <div style={{ marginBottom: '24px' }}>
          <div style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
            <Link to="/seller/dashboard" style={{ color: 'var(--text-secondary)' }}>Seller Hub</Link> / Payouts
          </div>
          <h1 style={{ fontSize: '22px', fontWeight: 700, marginTop: '2px' }}>Earnings & Payouts</h1>
        </div>

        {loading ? (
          <div style={{ textAlign: 'center', padding: '60px 0', color: 'var(--text-muted)' }}>
            <i className="ti ti-loader-2 ti-spin" style={{ fontSize: '24px' }} />
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>
            {/* Balance cards */}
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px' }}>
              <div style={{
                background: 'var(--surface-1)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius-xl)',
                padding: '24px',
              }}>
                <div style={{ fontSize: '12px', color: 'var(--text-secondary)', marginBottom: '4px' }}>
                  Available for Payout
                </div>
                <div style={{ fontSize: '26px', fontWeight: 700, color: 'var(--text-success)' }}>
                  {formatMoney(data?.available_payout)}
                </div>
                <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '8px' }}>
                  Disbursed automatically to your connected account.
                </div>
              </div>

              <div style={{
                background: 'var(--surface-1)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius-xl)',
                padding: '24px',
              }}>
                <div style={{ fontSize: '12px', color: 'var(--text-secondary)', marginBottom: '4px' }}>
                  Platform Fee Model
                </div>
                <div style={{ fontSize: '26px', fontWeight: 700, color: 'var(--text-primary)' }}>
                  10%
                </div>
                <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '8px' }}>
                  90% seller split computed and credited directly to dual-entry ledger upon delivery.
                </div>
              </div>
            </div>

            {/* Payouts History */}
            <div style={{
              background: 'var(--surface-1)',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius-xl)',
              padding: '24px',
            }}>
              <h2 style={{ fontSize: '16px', fontWeight: 700, marginBottom: '16px' }}>Payout Activity</h2>

              {(!data?.payouts || data.payouts.length === 0) ? (
                <div style={{ textAlign: 'center', padding: '24px 0', color: 'var(--text-muted)', fontSize: '13px' }}>
                  No past transfers yet. Payouts initiate as soon as orders transition to delivered.
                </div>
              ) : (
                <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '13px', textAlign: 'left' }}>
                  <thead>
                    <tr style={{ borderBottom: '1px solid var(--border)', color: 'var(--text-muted)' }}>
                      <th style={{ padding: '8px 12px' }}>Payout ID</th>
                      <th style={{ padding: '8px 12px' }}>Amount</th>
                      <th style={{ padding: '8px 12px' }}>Status</th>
                      <th style={{ padding: '8px 12px' }}>Date</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.payouts.map((p: any) => (
                      <tr key={p.id} style={{ borderBottom: '1px solid var(--border)' }}>
                        <td style={{ padding: '12px' }}>{p.id.slice(0, 8)}</td>
                        <td style={{ padding: '12px', fontWeight: 600 }}>{formatMoney(p.amount)}</td>
                        <td style={{ padding: '12px' }}>
                          <span className="badge badge-success">{p.status}</span>
                        </td>
                        <td style={{ padding: '12px', color: 'var(--text-muted)' }}>
                          {new Date(p.created_at).toLocaleDateString()}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        )}
      </main>
    </div>
  );
};
