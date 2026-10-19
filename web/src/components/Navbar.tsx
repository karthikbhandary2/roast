import React, { useState, useEffect } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useAuth } from '../hooks/useAuth';
import { api } from '../api/client';

export const Navbar: React.FC<{ searchQuery?: string; onSearchChange?: (q: string) => void }> = ({
  searchQuery,
  onSearchChange,
}) => {
  const { user, logout, isSeller, isAdmin } = useAuth();
  const [cartCount, setCartCount] = useState<number>(0);
  const [animateBadge, setAnimateBadge] = useState<boolean>(false);
  const navigate = useNavigate();

  const fetchCartCount = () => {
    if (user) {
      api.getCart()
        .then((cart) => {
          const totalQty = (cart?.items || []).reduce((acc: number, item: any) => acc + (item.quantity || 1), 0);
          setCartCount((prev) => {
            if (prev !== totalQty) {
              setAnimateBadge(true);
              setTimeout(() => setAnimateBadge(false), 500);
            }
            return totalQty;
          });
        })
        .catch(() => setCartCount(0));
    } else {
      setCartCount(0);
    }
  };

  useEffect(() => {
    fetchCartCount();

    const handleCartUpdated = (e: any) => {
      if (typeof e?.detail?.count === 'number') {
        setCartCount(e.detail.count);
        setAnimateBadge(true);
        setTimeout(() => setAnimateBadge(false), 500);
      } else {
        fetchCartCount();
      }
    };

    window.addEventListener('cart-updated', handleCartUpdated);
    return () => {
      window.removeEventListener('cart-updated', handleCartUpdated);
    };
  }, [user]);

  return (
    <header style={{
      background: 'var(--surface-1)',
      borderBottom: '1px solid var(--border)',
      position: 'sticky',
      top: 0,
      zIndex: 50,
      boxShadow: 'var(--shadow-sm)'
    }}>
      <div className="container" style={{
        display: 'flex',
        alignItems: 'center',
        gap: '16px',
        height: '64px',
      }}>
        {/* Brand */}
        <Link to="/" style={{
          fontSize: '18px',
          fontWeight: 700,
          fontFamily: 'var(--font-serif)',
          letterSpacing: '-0.02em',
          display: 'flex',
          alignItems: 'center',
          gap: '8px'
        }}>
          <span style={{
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: '28px',
            height: '28px',
            borderRadius: '50%',
            background: 'var(--fill-accent)',
            color: '#fff',
            fontSize: '14px'
          }}>☕</span>
          Roast co.
        </Link>

        {/* Global Search Bar */}
        <div style={{
          flex: 1,
          maxWidth: '480px',
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
          background: 'var(--surface-2)',
          border: '1px solid var(--border-strong)',
          borderRadius: 'var(--radius)',
          padding: '0 12px',
          height: '38px',
        }}>
          <i className="ti ti-search" style={{ color: 'var(--text-muted)', fontSize: '16px' }} />
          <input
            type="text"
            placeholder="Search coffees, origins, roasters..."
            value={searchQuery !== undefined ? searchQuery : ''}
            onChange={(e) => onSearchChange?.(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && searchQuery !== undefined) {
                navigate(`/?q=${encodeURIComponent(searchQuery)}`);
              }
            }}
            style={{
              border: 'none',
              background: 'transparent',
              outline: 'none',
              width: '100%',
              fontSize: '13px',
              color: 'var(--text-primary)',
            }}
          />
        </div>

        {/* Navigation links */}
        <nav style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: '20px' }}>
          <Link to="/" style={{ fontSize: '13px', fontWeight: 500, color: 'var(--text-secondary)' }}>
            Catalog
          </Link>

          {user && (
            <Link to="/orders" style={{ fontSize: '13px', fontWeight: 500, color: 'var(--text-secondary)' }}>
              My Orders
            </Link>
          )}

          {isSeller ? (
            <Link to="/seller/dashboard" style={{
              fontSize: '13px',
              fontWeight: 600,
              color: 'var(--text-accent)',
              display: 'flex',
              alignItems: 'center',
              gap: '4px'
            }}>
              <i className="ti ti-building-store" /> Seller Hub
            </Link>
          ) : user ? (
            <Link to="/seller/onboarding" style={{ fontSize: '13px', fontWeight: 500, color: 'var(--text-secondary)' }}>
              Become a Seller
            </Link>
          ) : null}

          {isAdmin && (
            <Link to="/admin" style={{
              fontSize: '13px',
              fontWeight: 600,
              color: 'var(--text-primary)',
              background: 'var(--surface-3)',
              padding: '4px 10px',
              borderRadius: 'var(--radius-sm)'
            }}>
              Admin
            </Link>
          )}

          {/* Cart Icon */}
          <Link to="/cart" style={{ position: 'relative', display: 'flex', alignItems: 'center', color: 'var(--text-primary)' }} title="Cart">
            <i className="ti ti-shopping-cart" style={{ fontSize: '22px' }} />
            {cartCount > 0 && (
              <span
                className={animateBadge ? 'cart-badge-animate' : ''}
                style={{
                  position: 'absolute',
                  top: '-6px',
                  right: '-8px',
                  background: 'var(--fill-accent)',
                  color: 'var(--on-accent)',
                  fontSize: '10px',
                  fontWeight: 700,
                  minWidth: '18px',
                  height: '18px',
                  padding: '0 4px',
                  borderRadius: 'var(--radius-pill)',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  boxShadow: '0 0 0 2px var(--surface-1)',
                  transition: 'transform 0.2s ease',
                }}
              >
                {cartCount}
              </span>
            )}
          </Link>

          {/* User Profile / Auth */}
          {user ? (
            <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
              <span style={{ fontSize: '13px', color: 'var(--text-secondary)' }} title={user.email}>
                {user.display_name}
              </span>
              <button
                onClick={logout}
                style={{
                  background: 'none',
                  border: 'none',
                  color: 'var(--text-muted)',
                  fontSize: '13px',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '4px'
                }}
                title="Log out"
              >
                <i className="ti ti-logout" />
              </button>
            </div>
          ) : (
            <Link to="/login" className="btn-primary" style={{ height: '34px', padding: '0 14px', fontSize: '13px' }}>
              Sign In
            </Link>
          )}
        </nav>
      </div>
    </header>
  );
};
