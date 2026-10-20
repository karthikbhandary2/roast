import React, { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { api, formatMoney } from '../../api/client';
import { SellerDashboard } from '../../types';
import { Navbar } from '../../components/Navbar';
import { useAuth } from '../../hooks/useAuth';

export const SellerDashboardPage: React.FC = () => {
  const { user } = useAuth();
  const [data, setData] = useState<SellerDashboard | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.getSellerDashboard()
      .then((d) => setData(d))
      .catch((err) => console.error('Error fetching seller dashboard', err))
      .finally(() => setLoading(false));
  }, []);

  return (
    <div>
      <Navbar />

      <main className="container" style={{ padding: '24px 20px 60px' }}>
        {/* Seller Subheader */}
        <div style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          marginBottom: '20px',
        }}>
          <div>
            <h1 style={{ fontSize: '22px', fontWeight: 700 }}>Seller Hub</h1>
            <p style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>
              Welcome back, {user?.display_name || 'Roaster'}
            </p>
          </div>

          <div style={{ display: 'flex', gap: '8px' }}>
            <Link to="/seller/listings" className="btn-secondary" style={{ height: '36px', fontSize: '13px' }}>
              <i className="ti ti-list" /> My Listings
            </Link>
            <Link to="/seller/orders" className="btn-secondary" style={{ height: '36px', fontSize: '13px' }}>
              <i className="ti ti-package" /> Orders
            </Link>
            <Link to="/seller/payouts" className="btn-secondary" style={{ height: '36px', fontSize: '13px' }}>
              <i className="ti ti-wallet" /> Payouts
            </Link>
          </div>
        </div>

        {loading ? (
          <div style={{ textAlign: 'center', padding: '60px 0', color: 'var(--text-muted)' }}>
            <i className="ti ti-loader-2 ti-spin" style={{ fontSize: '24px' }} />
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>
            {/* 4 Metric cards matching seller_dashboard_wireframe.html */}
            <div className="grid-4col-responsive">
              <div style={{
                background: 'var(--surface-1)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius-lg)',
                padding: '20px',
              }}>
                <div style={{ fontSize: '12px', color: 'var(--text-secondary)', marginBottom: '4px' }}>
                  Available Balance
                </div>
                <div style={{ fontSize: '22px', fontWeight: 700, color: 'var(--text-success)' }}>
                  {formatMoney(data?.available_payout)}
                </div>
                <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginTop: '4px' }}>
                  Ready for instant payout
                </div>
              </div>

              <div style={{
                background: 'var(--surface-1)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius-lg)',
                padding: '20px',
              }}>
                <div style={{ fontSize: '12px', color: 'var(--text-secondary)', marginBottom: '4px' }}>
                  Pending In Escrow
                </div>
                <div style={{ fontSize: '22px', fontWeight: 700, color: 'var(--text-primary)' }}>
                  {formatMoney(data?.pending_payout)}
                </div>
                <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginTop: '4px' }}>
                  Awaiting delivery confirmation
                </div>
              </div>

              <div style={{
                background: 'var(--surface-1)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius-lg)',
                padding: '20px',
              }}>
                <div style={{ fontSize: '12px', color: 'var(--text-secondary)', marginBottom: '4px' }}>
                  Awaiting Fulfillment
                </div>
                <div style={{ fontSize: '22px', fontWeight: 700, color: 'var(--text-accent)' }}>
                  {data?.orders_awaiting_fulfillment || 0}
                </div>
                <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginTop: '4px' }}>
                  Orders ready to ship
                </div>
              </div>

              <div style={{
                background: 'var(--surface-1)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius-lg)',
                padding: '20px',
              }}>
                <div style={{ fontSize: '12px', color: 'var(--text-secondary)', marginBottom: '4px' }}>
                  Low Stock Listings
                </div>
                <div style={{ fontSize: '22px', fontWeight: 700, color: 'var(--text-warning)' }}>
                  {data?.listings_low_on_stock || 0}
                </div>
                <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginTop: '4px' }}>
                  ≤ 5 bags remaining
                </div>
              </div>
            </div>

            {/* Needs Attention Section matching wireframe */}
            <div style={{
              background: 'var(--surface-1)',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius-xl)',
              padding: '24px',
            }}>
              <h2 style={{ fontSize: '15px', fontWeight: 700, marginBottom: '16px' }}>Needs Attention</h2>

              <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
                <div style={{
                  display: 'flex',
                  justifyContent: 'space-between',
                  alignItems: 'center',
                  padding: '14px 16px',
                  background: 'var(--surface-2)',
                  borderRadius: 'var(--radius)',
                  border: '1px solid var(--border)',
                }}>
                  <div style={{ fontSize: '13px', display: 'flex', alignItems: 'center', gap: '8px' }}>
                    <i className="ti ti-alert-circle" style={{ color: 'var(--text-accent)' }} />
                    <span>{data?.orders_awaiting_fulfillment || 0} order(s) awaiting fulfillment</span>
                  </div>
                  <Link to="/seller/orders" style={{ fontSize: '12px', fontWeight: 600, color: 'var(--text-accent)' }}>
                    Review Orders →
                  </Link>
                </div>

                <div style={{
                  display: 'flex',
                  justifyContent: 'space-between',
                  alignItems: 'center',
                  padding: '14px 16px',
                  background: 'var(--surface-2)',
                  borderRadius: 'var(--radius)',
                  border: '1px solid var(--border)',
                }}>
                  <div style={{ fontSize: '13px', display: 'flex', alignItems: 'center', gap: '8px' }}>
                    <i className="ti ti-boxes" style={{ color: 'var(--text-warning)' }} />
                    <span>{data?.listings_low_on_stock || 0} listing(s) low on inventory</span>
                  </div>
                  <Link to="/seller/listings" style={{ fontSize: '12px', fontWeight: 600, color: 'var(--text-warning)' }}>
                    Manage Inventory →
                  </Link>
                </div>
              </div>
            </div>
          </div>
        )}
      </main>
    </div>
  );
};
