import React, { useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { useAuth } from '../hooks/useAuth';

export const AuthPage: React.FC = () => {
  const [isRegister, setIsRegister] = useState(false);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const { login, register } = useAuth();
  const navigate = useNavigate();

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setLoading(true);

    try {
      if (isRegister) {
        await register(email, password, displayName);
      } else {
        await login(email, password);
      }
      navigate('/');
    } catch (err: any) {
      setError(err.message || 'Authentication failed. Please check credentials.');
    } finally {
      setLoading(false);
    }
  };

  const fillQuick = (e: string, p: string, d?: string) => {
    setEmail(e);
    setPassword(p);
    if (d) setDisplayName(d);
  };

  return (
    <div style={{
      minHeight: '100vh',
      display: 'flex',
      flexDirection: 'column',
      justifyContent: 'center',
      alignItems: 'center',
      padding: '20px',
      background: 'var(--surface-2)',
    }}>
      <Link to="/" style={{
        fontSize: '22px',
        fontWeight: 700,
        fontFamily: 'var(--font-serif)',
        marginBottom: '24px',
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
      }}>
        <span style={{
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: '32px',
          height: '32px',
          borderRadius: '50%',
          background: 'var(--fill-accent)',
          color: '#fff',
          fontSize: '16px',
        }}>☕</span>
        Roast co.
      </Link>

      <div style={{
        width: '100%',
        maxWidth: '400px',
        background: 'var(--surface-1)',
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius-xl)',
        padding: '32px',
        boxShadow: 'var(--shadow-md)',
      }}>
        {/* Toggle tabs matching wireframe */}
        <div style={{
          display: 'flex',
          borderBottom: '1px solid var(--border)',
          marginBottom: '24px',
        }}>
          <button
            type="button"
            onClick={() => { setIsRegister(false); setError(null); }}
            style={{
              flex: 1,
              paddingBottom: '10px',
              border: 'none',
              background: 'none',
              fontWeight: !isRegister ? 700 : 500,
              fontSize: '14px',
              color: !isRegister ? 'var(--text-primary)' : 'var(--text-muted)',
              borderBottom: !isRegister ? '2px solid var(--fill-accent)' : 'none',
            }}
          >
            Sign In
          </button>
          <button
            type="button"
            onClick={() => { setIsRegister(true); setError(null); }}
            style={{
              flex: 1,
              paddingBottom: '10px',
              border: 'none',
              background: 'none',
              fontWeight: isRegister ? 700 : 500,
              fontSize: '14px',
              color: isRegister ? 'var(--text-primary)' : 'var(--text-muted)',
              borderBottom: isRegister ? '2px solid var(--fill-accent)' : 'none',
            }}
          >
            Create Account
          </button>
        </div>

        {error && (
          <div style={{
            background: 'var(--badge-danger-bg)',
            border: '1px solid #fad2d2',
            color: 'var(--text-danger)',
            borderRadius: 'var(--radius)',
            padding: '10px 14px',
            marginBottom: '16px',
            fontSize: '13px',
          }}>
            {error}
          </div>
        )}

        <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
          {isRegister && (
            <div>
              <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '6px' }}>
                Full Name *
              </label>
              <input
                type="text"
                required
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                placeholder="Jane Roaster"
                className="input-control"
              />
            </div>
          )}

          <div>
            <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '6px' }}>
              Email Address *
            </label>
            <input
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="you@example.com"
              className="input-control"
            />
          </div>

          <div>
            <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '6px' }}>
              Password *
            </label>
            <input
              type="password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
              className="input-control"
            />
          </div>

          <button
            type="submit"
            disabled={loading}
            className="btn-accent"
            style={{ width: '100%', height: '44px', fontWeight: 600, marginTop: '8px' }}
          >
            {loading ? 'Authenticating...' : isRegister ? 'Create Account' : 'Sign In'}
          </button>
        </form>

        {/* Quick test accounts */}
        <div style={{
          marginTop: '24px',
          paddingTop: '20px',
          borderTop: '1px solid var(--border)',
          fontSize: '12px',
          color: 'var(--text-secondary)',
        }}>
          <div style={{ fontWeight: 600, marginBottom: '8px', color: 'var(--text-muted)' }}>
            Quick Dev Accounts:
          </div>
          <div style={{ display: 'flex', gap: '6px', flexWrap: 'wrap' }}>
            <button
              type="button"
              onClick={() => { setIsRegister(false); fillQuick('admin@example.com', 'admin-dev-password'); }}
              className="badge badge-neutral"
              style={{ cursor: 'pointer', border: '1px solid var(--border)' }}
            >
              Dev Admin
            </button>
            <button
              type="button"
              onClick={() => { setIsRegister(false); fillQuick('buyer1@example.com', 'password123'); }}
              className="badge badge-neutral"
              style={{ cursor: 'pointer', border: '1px solid var(--border)' }}
            >
              Seller (Blue Tokai)
            </button>
            <button
              type="button"
              onClick={() => { setIsRegister(false); fillQuick('buyer2@example.com', 'password123'); }}
              className="badge badge-neutral"
              style={{ cursor: 'pointer', border: '1px solid var(--border)' }}
            >
              Buyer (Jane)
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
