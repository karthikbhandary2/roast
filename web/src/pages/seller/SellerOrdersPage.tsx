import React, { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { api, formatMoney } from '../../api/client';
import { SubOrder } from '../../types';
import { Navbar } from '../../components/Navbar';

export const SellerOrdersPage: React.FC = () => {
  const [orders, setOrders] = useState<SubOrder[]>([]);
  const [loading, setLoading] = useState(true);
  const [shippingId, setShippingId] = useState<string | null>(null);
  const [trackingNumber, setTrackingNumber] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    loadOrders();
  }, []);

  const loadOrders = async () => {
    setLoading(true);
    try {
      const res = await api.getSellerOrders();
      setOrders(res || []);
    } catch (err) {
      console.error('Failed to load seller orders', err);
      setOrders([]);
    } finally {
      setLoading(false);
    }
  };

  const handleMarkShipped = async (subOrderId: string) => {
    if (!trackingNumber.trim()) {
      alert('Please enter a tracking number');
      return;
    }
    setSubmitting(true);
    try {
      await api.markOrderShipped(subOrderId, trackingNumber.trim());
      setShippingId(null);
      setTrackingNumber('');
      await loadOrders();
    } catch (err: any) {
      alert(`Could not mark shipped: ${err.message}`);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div>
      <Navbar />

      <main className="container" style={{ padding: '24px 20px 60px' }}>
        <div style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          marginBottom: '20px',
        }}>
          <div>
            <div style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
              <Link to="/seller/dashboard" style={{ color: 'var(--text-secondary)' }}>Seller Hub</Link> / Orders
            </div>
            <h1 style={{ fontSize: '22px', fontWeight: 700, marginTop: '2px' }}>Fulfillment Orders</h1>
          </div>
        </div>

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
            <div style={{ fontSize: '32px', marginBottom: '12px' }}>☕</div>
            <h3 style={{ fontSize: '16px', fontWeight: 600, marginBottom: '6px' }}>No orders awaiting fulfillment</h3>
            <p style={{ fontSize: '13px', color: 'var(--text-muted)' }}>
              New customer orders will appear here automatically when placed.
            </p>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
            {orders.map((o) => {
              const isConfirmed = o.status === 'SUB_ORDER_STATUS_CONFIRMED';
              const isShipped = o.status === 'SUB_ORDER_STATUS_SHIPPED';
              const isDelivered = o.status === 'SUB_ORDER_STATUS_DELIVERED';

              return (
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
                    marginBottom: '16px',
                    paddingBottom: '14px',
                    borderBottom: '1px solid var(--border)',
                  }}>
                    <div>
                      <span style={{ fontSize: '14px', fontWeight: 700 }}>
                        Sub-order #{o.id.slice(0, 8)}
                      </span>
                      <span style={{ fontSize: '12px', color: 'var(--text-muted)', marginLeft: '12px' }}>
                        Parent Order #{o.parent_order_id.slice(0, 8)}
                      </span>
                    </div>

                    <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                      <span style={{ fontSize: '15px', fontWeight: 700 }}>
                        {formatMoney(o.subtotal)}
                      </span>
                      <span className={`badge ${
                        isDelivered ? 'badge-success' : isShipped ? 'badge-warning' : 'badge-neutral'
                      }`}>
                        {o.status.replace('SUB_ORDER_STATUS_', '')}
                      </span>
                    </div>
                  </div>

                  <div style={{ fontSize: '13px', color: 'var(--text-secondary)', marginBottom: '16px' }}>
                    Items: {o.items?.length || 1} line item(s) · Hold Reservation: {o.reservation_id ? o.reservation_id.slice(0, 8) : 'Committed'}
                  </div>

                  {o.tracking_number && (
                    <div style={{ fontSize: '12px', color: 'var(--text-accent)', marginBottom: '14px' }}>
                      🚚 Dispatched with carrier tracking: <strong>{o.tracking_number}</strong>
                    </div>
                  )}

                  {isConfirmed && (
                    <div>
                      {shippingId === o.id ? (
                        <div style={{ display: 'flex', gap: '10px', alignItems: 'center' }}>
                          <input
                            type="text"
                            placeholder="Enter carrier tracking number (e.g. TRACK-999)"
                            value={trackingNumber}
                            onChange={(e) => setTrackingNumber(e.target.value)}
                            className="input-control"
                            style={{ maxWidth: '320px', height: '36px' }}
                          />
                          <button
                            type="button"
                            disabled={submitting}
                            onClick={() => handleMarkShipped(o.id)}
                            className="btn-accent"
                            style={{ height: '36px', fontSize: '13px' }}
                          >
                            {submitting ? 'Updating...' : 'Confirm Shipment'}
                          </button>
                          <button
                            type="button"
                            onClick={() => setShippingId(null)}
                            className="btn-secondary"
                            style={{ height: '36px', fontSize: '13px' }}
                          >
                            Cancel
                          </button>
                        </div>
                      ) : (
                        <button
                          type="button"
                          onClick={() => setShippingId(o.id)}
                          className="btn-accent"
                          style={{ height: '36px', fontSize: '13px' }}
                        >
                          📦 Mark as Shipped
                        </button>
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </main>
    </div>
  );
};
