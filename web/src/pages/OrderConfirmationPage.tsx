import React, { useState, useEffect } from 'react';
import { useParams, Link } from 'react-router-dom';
import { api, formatMoney } from '../api/client';
import { Order, SagaEvent } from '../types';
import { Navbar } from '../components/Navbar';

export const OrderConfirmationPage: React.FC = () => {
  const { id } = useParams<{ id: string }>();

  const [order, setOrder] = useState<Order | null>(null);
  const [events, setEvents] = useState<SagaEvent[]>([]);
  const [loading, setLoading] = useState(true);

  const reloadOrder = () => {
    if (!id) return;
    api.getOrder(id)
      .then((o) => setOrder(o))
      .catch((err) => console.error('Error loading order', err))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    if (!id) return;
    reloadOrder();

    // Listen for live Server-Sent Events (SSE) from the gateway
    const token = localStorage.getItem('token');
    const sseUrl = `/v1/orders/${id}/events${token ? `?token=${encodeURIComponent(token)}` : ''}`;
    
    const evtSource = new EventSource(sseUrl);
    evtSource.onmessage = (e) => {
      try {
        const payload = JSON.parse(e.data);
        setEvents((prev) => [...prev, payload]);
        // Reload order state whenever saga status updates
        reloadOrder();
      } catch {
        // Ping or format
      }
    };

    return () => {
      evtSource.close();
    };
  }, [id]);

  return (
    <div>
      <Navbar />

      <main className="container" style={{ padding: '32px 20px 60px', maxWidth: '540px' }}>

        {/* Order Confirmed / Saga Status View */}
        <div style={{
          background: 'var(--surface-1)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-xl)',
          padding: '28px',
          boxShadow: 'var(--shadow-sm)',
        }}>
            <div style={{
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              width: '40px',
              height: '40px',
              borderRadius: '50%',
              background: 'var(--badge-success-bg)',
              color: 'var(--text-success)',
              fontSize: '20px',
              marginBottom: '12px',
            }}>
              ✓
            </div>

            <div style={{ fontSize: '18px', fontWeight: 700, marginBottom: '2px' }}>
              Order Placed! {order?.total && `(${formatMoney(order.total)})`}
            </div>
            <div style={{ fontSize: '13px', color: 'var(--text-secondary)', marginBottom: '24px' }}>
              {loading ? 'Finalizing saga...' : `Order #${id?.slice(0, 8)} · Distributed Saga confirmed`}
            </div>

            {/* Saga Orchestration Timeline matching order_confirmation_saga_wireframe.html */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: '16px', marginBottom: '28px' }}>
              {/* Step 1 */}
              <div style={{ display: 'flex', gap: '12px', alignItems: 'flex-start' }}>
                <i className="ti ti-check" style={{ fontSize: '18px', color: 'var(--text-success)', marginTop: '2px' }} />
                <div>
                  <div style={{ fontSize: '13px', fontWeight: 600 }}>Inventory reserved</div>
                  <div style={{ fontSize: '11px', color: 'var(--text-muted)' }}>Held with deterministic row-level locks</div>
                </div>
              </div>

              {/* Step 2 */}
              <div style={{ display: 'flex', gap: '12px', alignItems: 'flex-start' }}>
                <i className="ti ti-check" style={{ fontSize: '18px', color: 'var(--text-success)', marginTop: '2px' }} />
                <div>
                  <div style={{ fontSize: '13px', fontWeight: 600 }}>Payment charged</div>
                  <div style={{ fontSize: '11px', color: 'var(--text-muted)' }}>
                    Dual-entry ledger entry committed · Platform fee split
                  </div>
                </div>
              </div>

              {/* Step 3 */}
              <div style={{ display: 'flex', gap: '12px', alignItems: 'flex-start' }}>
                <i className="ti ti-check" style={{ fontSize: '18px', color: 'var(--text-success)', marginTop: '2px' }} />
                <div>
                  <div style={{ fontSize: '13px', fontWeight: 600 }}>Sub-orders routed to roasters</div>
                  <div style={{ fontSize: '11px', color: 'var(--text-muted)' }}>
                    Confirmed via parallel saga orchestrator
                  </div>
                </div>
              </div>

              {/* Step 4 */}
              <div style={{ display: 'flex', gap: '12px', alignItems: 'flex-start' }}>
                <i className="ti ti-check" style={{ fontSize: '18px', color: 'var(--text-success)', marginTop: '2px' }} />
                <div>
                  <div style={{ fontSize: '13px', fontWeight: 600 }}>Notification dispatched</div>
                  <div style={{ fontSize: '11px', color: 'var(--text-muted)' }}>
                    Kafka event published to notification.events
                  </div>
                </div>
              </div>
            </div>

            {/* Real-time events received log */}
            {events.length > 0 && (
              <div style={{
                background: 'var(--surface-2)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius)',
                padding: '12px',
                fontSize: '11px',
                color: 'var(--text-secondary)',
                fontFamily: 'var(--font-mono)',
                marginBottom: '20px',
                maxHeight: '120px',
                overflowY: 'auto',
              }}>
                <div style={{ fontWeight: 600, marginBottom: '4px', color: 'var(--text-primary)' }}>
                  Live Saga SSE Feed:
                </div>
                {events.map((ev, i) => (
                  <div key={i}>
                    [{new Date(ev.timestamp).toLocaleTimeString()}] {ev.event_type} - {ev.status}
                  </div>
                ))}
              </div>
            )}

            <div style={{ display: 'flex', gap: '12px' }}>
              <Link to="/orders" className="btn-accent" style={{ flex: 1, textAlign: 'center', height: '40px', lineHeight: '40px' }}>
                Track Order →
              </Link>
              <Link to="/" className="btn-secondary" style={{ flex: 1, textAlign: 'center', height: '40px', lineHeight: '40px' }}>
                Continue Shopping
              </Link>
            </div>
          </div>
      </main>
    </div>
  );
};
