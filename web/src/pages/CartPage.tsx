import React, { useState, useEffect } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { api, formatMoney } from '../api/client';
import { Cart, CartItem } from '../types';
import { Navbar } from '../components/Navbar';
import { useAuth } from '../hooks/useAuth';

export const CartPage: React.FC = () => {
  const { user } = useAuth();
  const navigate = useNavigate();

  const [cart, setCart] = useState<Cart | null>(null);
  const [loading, setLoading] = useState(true);
  const [updatingId, setUpdatingId] = useState<string | null>(null);

  useEffect(() => {
    if (!user) {
      setLoading(false);
      return;
    }
    loadCart();
  }, [user]);

  const loadCart = async () => {
    setLoading(true);
    try {
      const c = await api.getCart();
      setCart(c);
    } catch (err) {
      console.error('Failed to load cart', err);
      setCart({ buyer_id: user?.id || '', items: [] });
    } finally {
      setLoading(false);
    }
  };

  const updateQuantity = async (productId: string, newQty: number) => {
    setUpdatingId(productId);
    try {
      const updated = await api.updateCartItem(productId, newQty);
      setCart(updated);
      const totalQty = (updated?.items || []).reduce((acc: number, item: any) => acc + (item.quantity || 1), 0);
      window.dispatchEvent(new CustomEvent('cart-updated', { detail: { count: totalQty } }));
    } catch (err: any) {
      alert(`Could not update cart: ${err.message}`);
    } finally {
      setUpdatingId(null);
    }
  };

  if (!user) {
    return (
      <div>
        <Navbar />
        <div className="container" style={{ padding: '60px 20px', textAlign: 'center' }}>
          <h2>Your Cart is Waiting</h2>
          <p style={{ color: 'var(--text-muted)', margin: '12px 0 20px' }}>
            Please sign in to view your cart and checkout.
          </p>
          <Link to="/login" className="btn-primary">Sign In</Link>
        </div>
      </div>
    );
  }

  const items: CartItem[] = cart?.items || [];
  const subtotalMinor = items.reduce((acc, item) => {
    const minor = Number(item.unit_price?.amount_minor || 0);
    return acc + minor * item.quantity;
  }, 0);

  const currencyCode = items[0]?.unit_price?.currency_code || 'USD';

  return (
    <div>
      <Navbar />

      <main className="container" style={{ padding: '24px 20px 60px' }}>
        <h1 style={{ fontSize: '22px', fontWeight: 700, marginBottom: '20px' }}>Shopping Cart</h1>

        {/* Reservation timer banner matching cart_wireframe.html */}
        <div style={{
          background: 'var(--badge-warning-bg)',
          border: '1px solid #fae2c8',
          borderRadius: 'var(--radius)',
          padding: '12px 16px',
          display: 'flex',
          alignItems: 'center',
          gap: '10px',
          marginBottom: '24px',
          fontSize: '13px',
          color: 'var(--text-warning)',
        }}>
          <i className="ti ti-clock" style={{ fontSize: '18px' }} />
          <div>
            <strong>10-minute hold active:</strong> Items in your cart are temporarily reserved in stock while you complete checkout.
          </div>
        </div>

        {loading ? (
          <div style={{ textAlign: 'center', padding: '40px 0', color: 'var(--text-muted)' }}>
            <i className="ti ti-loader-2 ti-spin" style={{ fontSize: '24px', marginBottom: '8px' }} />
            Loading cart...
          </div>
        ) : items.length === 0 ? (
          <div style={{
            background: 'var(--surface-1)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-xl)',
            padding: '48px 20px',
            textAlign: 'center',
          }}>
            <div style={{ fontSize: '36px', marginBottom: '12px' }}>🛒</div>
            <h3 style={{ fontSize: '16px', fontWeight: 600, marginBottom: '6px' }}>Your cart is empty</h3>
            <p style={{ fontSize: '13px', color: 'var(--text-muted)', marginBottom: '16px' }}>
              Explore our fresh single origins and award-winning blends.
            </p>
            <Link to="/" className="btn-primary">Browse Coffees</Link>
          </div>
        ) : (
          <div className="grid-cart-responsive">
            {/* Items list */}
            <div style={{
              background: 'var(--surface-1)',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius-xl)',
              padding: '20px',
            }}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
                {items.map((item) => {
                  const lineTotalMinor = Number(item.unit_price?.amount_minor || 0) * item.quantity;
                  const isUpdating = updatingId === item.product_id;

                  return (
                    <div
                      key={item.product_id}
                      className="cart-item-row"
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: '16px',
                        paddingBottom: '20px',
                        borderBottom: '1px solid var(--border)',
                      }}
                    >
                      <div style={{
                        width: '64px',
                        height: '64px',
                        borderRadius: 'var(--radius)',
                        background: 'var(--surface-2)',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        fontSize: '24px',
                        border: '1px solid var(--border)',
                        flexShrink: 0,
                      }}>
                        ☕
                      </div>

                      <div style={{ flex: 1 }}>
                        <Link
                          to={`/products/${item.product_id}`}
                          style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text-primary)' }}
                        >
                          Fresh Coffee Roast
                        </Link>
                        <div style={{ fontSize: '12px', color: 'var(--text-secondary)', marginTop: '2px' }}>
                          Seller: {item.seller_id.slice(0, 8)}...
                        </div>
                        <div style={{ fontSize: '13px', fontWeight: 600, color: 'var(--text-primary)', marginTop: '4px' }}>
                          {formatMoney(item.unit_price)} each
                        </div>
                      </div>

                      <div className="cart-item-controls" style={{ display: 'flex', alignItems: 'center', gap: '16px' }}>
                        {/* Quantity controls */}
                        <div style={{
                          display: 'flex',
                          alignItems: 'center',
                          border: '1px solid var(--border-strong)',
                          borderRadius: 'var(--radius)',
                          background: 'var(--surface-1)',
                        }}>
                          <button
                            type="button"
                            disabled={isUpdating}
                            onClick={() => updateQuantity(item.product_id, item.quantity - 1)}
                            style={{ border: 'none', background: 'none', padding: '4px 10px', fontSize: '14px' }}
                          >
                            -
                          </button>
                          <span style={{ minWidth: '24px', textAlign: 'center', fontSize: '13px', fontWeight: 600 }}>
                            {item.quantity}
                          </span>
                          <button
                            type="button"
                            disabled={isUpdating}
                            onClick={() => updateQuantity(item.product_id, item.quantity + 1)}
                            style={{ border: 'none', background: 'none', padding: '4px 10px', fontSize: '14px' }}
                          >
                            +
                          </button>
                        </div>

                        {/* Line total */}
                        <div style={{ minWidth: '70px', textAlign: 'right', fontSize: '14px', fontWeight: 700 }}>
                          {formatMoney({ currency_code: currencyCode, amount_minor: lineTotalMinor })}
                        </div>

                        {/* Delete item */}
                        <button
                          type="button"
                          onClick={() => updateQuantity(item.product_id, 0)}
                          style={{ border: 'none', background: 'none', color: 'var(--text-muted)', cursor: 'pointer' }}
                          title="Remove"
                        >
                          <i className="ti ti-trash" style={{ fontSize: '16px' }} />
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>

            {/* Order summary card matching wireframe */}
            <div style={{
              background: 'var(--surface-1)',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius-xl)',
              padding: '24px',
              height: 'fit-content',
            }}>
              <h2 style={{ fontSize: '16px', fontWeight: 700, marginBottom: '16px' }}>Order Summary</h2>

              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '13px', marginBottom: '10px' }}>
                <span style={{ color: 'var(--text-secondary)' }}>Items subtotal</span>
                <span>{formatMoney({ currency_code: currencyCode, amount_minor: subtotalMinor })}</span>
              </div>

              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '13px', marginBottom: '10px' }}>
                <span style={{ color: 'var(--text-secondary)' }}>Shipping</span>
                <span style={{ color: 'var(--text-success)', fontWeight: 600 }}>FREE</span>
              </div>

              <div style={{
                display: 'flex',
                justifyContent: 'space-between',
                fontSize: '16px',
                fontWeight: 700,
                paddingTop: '14px',
                marginTop: '14px',
                borderTop: '1px solid var(--border)',
                marginBottom: '20px',
              }}>
                <span>Total</span>
                <span>{formatMoney({ currency_code: currencyCode, amount_minor: subtotalMinor })}</span>
              </div>

              <button
                type="button"
                onClick={() => navigate('/checkout')}
                className="btn-accent"
                style={{ width: '100%', height: '44px', fontWeight: 600 }}
              >
                Proceed to Checkout →
              </button>
            </div>
          </div>
        )}
      </main>
    </div>
  );
};
