import React, { useState, useEffect } from 'react';
import { api, formatMoney } from '../api/client';
import { Navbar } from '../components/Navbar';
import { useAuth } from '../hooks/useAuth';

export const AdminPage: React.FC = () => {
  const { isAdmin } = useAuth();
  const [tab, setTab] = useState<'sellers' | 'inventory' | 'orders'>('sellers');

  const [sellers, setSellers] = useState<any[]>([]);
  const [inventory, setInventory] = useState<any[]>([]);
  const [orders, setOrders] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState<string | null>(null);

  useEffect(() => {
    loadData();
  }, [tab]);

  const loadData = async () => {
    setLoading(true);
    try {
      if (tab === 'sellers') {
        const s = await api.getAdminSellers('pending_review');
        setSellers(s || []);
      } else if (tab === 'inventory') {
        const inv = await api.getAdminInventory();
        setInventory(inv || []);
      } else {
        const ord = await api.getAdminOrders();
        setOrders(ord || []);
      }
    } catch (err) {
      console.error('Failed to load admin data', err);
    } finally {
      setLoading(false);
    }
  };

  const handleReviewSeller = async (userId: string, status: 'active' | 'rejected') => {
    setActionLoading(userId);
    try {
      await api.reviewSeller(userId, status, `Reviewed and ${status} by admin.`);
      await loadData();
    } catch (err: any) {
      alert(`Could not review seller: ${err.message}`);
    } finally {
      setActionLoading(null);
    }
  };

  if (!isAdmin) {
    return (
      <div>
        <Navbar />
        <div className="container" style={{ padding: '60px 20px', textAlign: 'center' }}>
          <h2>Access Restricted</h2>
          <p style={{ color: 'var(--text-muted)', margin: '12px 0' }}>
            This page is restricted to platform administrators.
          </p>
        </div>
      </div>
    );
  }

  return (
    <div>
      <Navbar />

      <main className="container" style={{ padding: '24px 20px 60px' }}>
        <h1 style={{ fontSize: '22px', fontWeight: 700, marginBottom: '20px' }}>Admin Operations</h1>

        {/* Tabs matching admin_panel_wireframe.html */}
        <div style={{
          display: 'flex',
          gap: '8px',
          borderBottom: '1px solid var(--border)',
          marginBottom: '24px',
        }}>
          <button
            type="button"
            onClick={() => setTab('sellers')}
            style={{
              padding: '10px 16px',
              border: 'none',
              background: 'none',
              fontWeight: tab === 'sellers' ? 700 : 500,
              fontSize: '13px',
              color: tab === 'sellers' ? 'var(--text-accent)' : 'var(--text-secondary)',
              borderBottom: tab === 'sellers' ? '2px solid var(--fill-accent)' : 'none',
              cursor: 'pointer',
            }}
          >
            Sellers Queue
          </button>
          <button
            type="button"
            onClick={() => setTab('inventory')}
            style={{
              padding: '10px 16px',
              border: 'none',
              background: 'none',
              fontWeight: tab === 'inventory' ? 700 : 500,
              fontSize: '13px',
              color: tab === 'inventory' ? 'var(--text-accent)' : 'var(--text-secondary)',
              borderBottom: tab === 'inventory' ? '2px solid var(--fill-accent)' : 'none',
              cursor: 'pointer',
            }}
          >
            Platform Inventory
          </button>
          <button
            type="button"
            onClick={() => setTab('orders')}
            style={{
              padding: '10px 16px',
              border: 'none',
              background: 'none',
              fontWeight: tab === 'orders' ? 700 : 500,
              fontSize: '13px',
              color: tab === 'orders' ? 'var(--text-accent)' : 'var(--text-secondary)',
              borderBottom: tab === 'orders' ? '2px solid var(--fill-accent)' : 'none',
              cursor: 'pointer',
            }}
          >
            All Orders
          </button>
        </div>

        {loading ? (
          <div style={{ textAlign: 'center', padding: '60px 0', color: 'var(--text-muted)' }}>
            <i className="ti ti-loader-2 ti-spin" style={{ fontSize: '24px' }} />
          </div>
        ) : tab === 'sellers' ? (
          /* Sellers Review Queue */
          <div style={{
            background: 'var(--surface-1)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-xl)',
            overflow: 'hidden',
          }}>
            {sellers.length === 0 ? (
              <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--text-muted)', fontSize: '13px' }}>
                ✓ No sellers currently awaiting review.
              </div>
            ) : (
              <div className="table-responsive">
                <table style={{ width: '100%', minWidth: '600px', borderCollapse: 'collapse', textAlign: 'left', fontSize: '13px' }}>
                <thead>
                  <tr style={{ background: 'var(--surface-2)', borderBottom: '1px solid var(--border)' }}>
                    <th style={{ padding: '12px 16px', fontWeight: 600 }}>Business Name</th>
                    <th style={{ padding: '12px 16px', fontWeight: 600 }}>User ID</th>
                    <th style={{ padding: '12px 16px', fontWeight: 600 }}>Provider</th>
                    <th style={{ padding: '12px 16px', fontWeight: 600 }}>Payout Method</th>
                    <th style={{ padding: '12px 16px', fontWeight: 600, textAlign: 'right' }}>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {sellers.map((s) => (
                    <tr key={s.user_id} style={{ borderBottom: '1px solid var(--border)' }}>
                      <td style={{ padding: '14px 16px', fontWeight: 600 }}>{s.business_name}</td>
                      <td style={{ padding: '14px 16px', color: 'var(--text-muted)' }}>{s.user_id.slice(0, 8)}...</td>
                      <td style={{ padding: '14px 16px' }}>{s.provider?.replace('PAYMENT_PROVIDER_', '')}</td>
                      <td style={{ padding: '14px 16px' }}>{s.payout_method}</td>
                      <td style={{ padding: '14px 16px', textAlign: 'right' }}>
                        <div style={{ display: 'inline-flex', gap: '8px' }}>
                          <button
                            type="button"
                            disabled={actionLoading === s.user_id}
                            onClick={() => handleReviewSeller(s.user_id, 'active')}
                            className="btn-accent"
                            style={{ height: '32px', fontSize: '12px', padding: '0 12px' }}
                          >
                            Approve
                          </button>
                          <button
                            type="button"
                            disabled={actionLoading === s.user_id}
                            onClick={() => handleReviewSeller(s.user_id, 'rejected')}
                            className="btn-secondary"
                            style={{ height: '32px', fontSize: '12px', padding: '0 12px' }}
                          >
                            Reject
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              </div>
            )}
          </div>
        ) : tab === 'inventory' ? (
          /* Platform Inventory Table */
          <div style={{
            background: 'var(--surface-1)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-xl)',
            overflow: 'hidden',
          }}>
            <div className="table-responsive">
              <table style={{ width: '100%', minWidth: '600px', borderCollapse: 'collapse', textAlign: 'left', fontSize: '13px' }}>
                <thead>
                  <tr style={{ background: 'var(--surface-2)', borderBottom: '1px solid var(--border)' }}>
                    <th style={{ padding: '12px 16px', fontWeight: 600 }}>Coffee</th>
                    <th style={{ padding: '12px 16px', fontWeight: 600 }}>Seller ID</th>
                    <th style={{ padding: '12px 16px', fontWeight: 600 }}>Price</th>
                    <th style={{ padding: '12px 16px', fontWeight: 600 }}>Stock</th>
                    <th style={{ padding: '12px 16px', fontWeight: 600 }}>Status</th>
                  </tr>
                </thead>
                <tbody>
                  {inventory.map((item) => (
                    <tr key={item.id} style={{ borderBottom: '1px solid var(--border)' }}>
                      <td style={{ padding: '14px 16px', fontWeight: 600 }}>{item.name}</td>
                      <td style={{ padding: '14px 16px', color: 'var(--text-muted)' }}>{item.seller_id?.slice(0, 8)}...</td>
                      <td style={{ padding: '14px 16px' }}>{formatMoney(item.price)}</td>
                      <td style={{ padding: '14px 16px' }}>{item.stock_available}</td>
                      <td style={{ padding: '14px 16px' }}>
                        <span className="badge badge-success">{item.status?.replace('PRODUCT_STATUS_', '')}</span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        ) : (
          /* All Orders Table */
          <div style={{
            background: 'var(--surface-1)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-xl)',
            overflow: 'hidden',
          }}>
            <div className="table-responsive">
              <table style={{ width: '100%', minWidth: '600px', borderCollapse: 'collapse', textAlign: 'left', fontSize: '13px' }}>
                <thead>
                  <tr style={{ background: 'var(--surface-2)', borderBottom: '1px solid var(--border)' }}>
                    <th style={{ padding: '12px 16px', fontWeight: 600 }}>Order ID</th>
                    <th style={{ padding: '12px 16px', fontWeight: 600 }}>Buyer ID</th>
                    <th style={{ padding: '12px 16px', fontWeight: 600 }}>Total</th>
                    <th style={{ padding: '12px 16px', fontWeight: 600 }}>Status</th>
                    <th style={{ padding: '12px 16px', fontWeight: 600 }}>Date</th>
                  </tr>
                </thead>
                <tbody>
                  {orders.map((o) => (
                    <tr key={o.id} style={{ borderBottom: '1px solid var(--border)' }}>
                      <td style={{ padding: '14px 16px', fontWeight: 600 }}>{o.id?.slice(0, 8)}</td>
                      <td style={{ padding: '14px 16px', color: 'var(--text-muted)' }}>{o.buyer_id?.slice(0, 8)}...</td>
                      <td style={{ padding: '14px 16px', fontWeight: 600 }}>{formatMoney(o.total)}</td>
                      <td style={{ padding: '14px 16px' }}>
                        <span className="badge badge-success">{o.status?.replace('ORDER_STATUS_', '')}</span>
                      </td>
                      <td style={{ padding: '14px 16px', color: 'var(--text-muted)' }}>
                        {new Date(o.created_at).toLocaleDateString()}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </main>
    </div>
  );
};
