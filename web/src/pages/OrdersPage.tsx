import React, { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { api, formatMoney } from '../api/client';
import { Order } from '../types';
import { Navbar } from '../components/Navbar';
import { useAuth } from '../hooks/useAuth';

export const OrdersPage: React.FC = () => {
  const { user } = useAuth();
  const [orders, setOrders] = useState<Order[]>([]);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState<string | null>(null);

  useEffect(() => {
    if (!user) return;
    loadOrders();
  }, [user]);

  const loadOrders = async () => {
    setLoading(true);
    try {
      const res = await api.getOrders();
      setOrders(res || []);
    } catch (err) {
      console.error('Failed to load orders', err);
      setOrders([]);
    } finally {
      setLoading(false);
    }
  };

  const handleConfirmDelivery = async (orderId: string, subOrderId: string) => {
    setActionLoading(subOrderId);
    try {
      await api.confirmDelivery(orderId, subOrderId);
      await loadOrders();
    } catch (err: any) {
      alert(`Could not confirm delivery: ${err.message}`);
    } finally {
      setActionLoading(null);
    }
  };

  return (
    <div>
      <Navbar />

      <main className="container" style={{ padding: '24px 20px 60px', maxWidth: '840px' }}>
        <h1 style={{ fontSize: '22px', fontWeight: 700, marginBottom: '20px' }}>Order History & Tracking</h1>

        {loading ? (
          <div style={{ textAlign: 'center', padding: '60px 0', color: 'var(--text-muted)' }}>
            <i className="ti ti-loader-2 ti-spin" style={{ fontSize: '24px' }} />
          </div>
        ) : orders.length === 0 ? (
          <div style={{
            background: 'var(--surface-1)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-xl)',
            padding: '48px 20px',
            textAlign: 'center',
          }}>
            <div style={{ fontSize: '32px', marginBottom: '12px' }}>📦</div>
            <h3 style={{ fontSize: '16px', fontWeight: 600, marginBottom: '6px' }}>No orders yet</h3>
            <p style={{ fontSize: '13px', color: 'var(--text-muted)', marginBottom: '16px' }}>
              Your freshly roasted coffee deliveries will appear here.
            </p>
            <Link to="/" className="btn-primary">Explore Catalog</Link>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
            {orders.map((o) => (
              <div
                key={o.id}
                style={{
                  background: 'var(--surface-1)',
                  border: '1px solid var(--border)',
                  borderRadius: 'var(--radius-xl)',
                  padding: '24px',
                }}
              >
                <div style={{
                  display: 'flex',
                  justifyContent: 'space-between',
                  alignItems: 'center',
                  paddingBottom: '16px',
                  borderBottom: '1px solid var(--border)',
                  marginBottom: '16px',
                }}>
                  <div>
                    <Link
                      to={`/orders/${o.id}/confirmation`}
                      style={{ fontSize: '14px', fontWeight: 700, color: 'var(--text-primary)', textDecoration: 'none' }}
                      title="View order details and payment status"
                    >
                      Order #{o.id.slice(0, 8)}
                    </Link>
                    <span style={{ fontSize: '12px', color: 'var(--text-muted)', marginLeft: '12px' }}>
                      Placed {new Date(o.created_at).toLocaleDateString()}
                    </span>
                  </div>

                  <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                    <span style={{ fontSize: '15px', fontWeight: 700 }}>
                      {formatMoney(o.total)}
                    </span>
                    <span className={`badge ${
                      o.status.includes('CONFIRMED')
                        ? 'badge-success'
                        : o.status.includes('FAILED')
                        ? 'badge-danger'
                        : 'badge-warning'
                    }`}>
                      {o.status.replace('ORDER_STATUS_', '')}
                    </span>
                  </div>
                </div>

                {/* Sub-orders */}
                <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
                  {(o.sub_orders || []).map((so) => {
                    const isShipped = so.status === 'SUB_ORDER_STATUS_SHIPPED';
                    const isDelivered = so.status === 'SUB_ORDER_STATUS_DELIVERED';
                    const productId = so.items?.[0]?.product_id;

                    return (
                      <div
                        key={so.id}
                        style={{
                          background: 'var(--surface-2)',
                          borderRadius: 'var(--radius)',
                          padding: '16px',
                          border: '1px solid var(--border)',
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'space-between',
                          gap: '16px',
                          flexWrap: 'wrap',
                        }}
                      >
                        <div>
                          <div style={{ fontSize: '13px', fontWeight: 600 }}>
                            Sub-order #{so.id.slice(0, 8)} (Seller: {so.seller_id.slice(0, 8)}...)
                          </div>
                          <div style={{ fontSize: '12px', color: 'var(--text-secondary)', marginTop: '2px' }}>
                            {so.items?.length || 1} line item(s) · Subtotal: {formatMoney(so.subtotal)}
                          </div>
                          {so.tracking_number && (
                            <div style={{ fontSize: '11px', color: 'var(--text-accent)', marginTop: '4px' }}>
                              🚚 Tracking: <strong>{so.tracking_number}</strong>
                            </div>
                          )}
                        </div>

                        <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                          <span className={`badge ${
                            isDelivered ? 'badge-success' : isShipped ? 'badge-warning' : 'badge-neutral'
                          }`}>
                            {so.status.replace('SUB_ORDER_STATUS_', '')}
                          </span>

                          {isShipped && (
                            <button
                              type="button"
                              disabled={actionLoading === so.id}
                              onClick={() => handleConfirmDelivery(o.id, so.id)}
                              className="btn-accent"
                              style={{ height: '32px', fontSize: '12px', padding: '0 12px' }}
                            >
                              {actionLoading === so.id ? 'Confirming...' : 'Confirm Delivery'}
                            </button>
                          )}

                          {isDelivered && productId && (
                            <Link
                              to={`/products/${productId}`}
                              className="btn-secondary"
                              style={{ height: '32px', fontSize: '12px', padding: '0 12px' }}
                            >
                              ★ Leave Review
                            </Link>
                          )}
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            ))}
          </div>
        )}
      </main>
    </div>
  );
};
