import React, { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { api, formatMoney } from '../../api/client';
import { Product } from '../../types';
import { Navbar } from '../../components/Navbar';

export const SellerListingsPage: React.FC = () => {
  const [listings, setListings] = useState<Product[]>([]);
  const [loading, setLoading] = useState(true);
  const [showAddModal, setShowAddModal] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // New Listing Form
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [currency, setCurrency] = useState('INR');
  const [priceMajor, setPriceMajor] = useState('550');
  const [batchLabel, setBatchLabel] = useState('BATCH-2026-A');
  const [initialStock, setInitialStock] = useState('50');

  useEffect(() => {
    loadListings();
  }, []);

  const loadListings = async () => {
    setLoading(true);
    try {
      const res = await api.getSellerListings();
      setListings(res || []);
    } catch (err) {
      console.error('Failed to load listings', err);
      setListings([]);
    } finally {
      setLoading(false);
    }
  };

  const handleCreateListing = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setError(null);

    const minor = currency === 'JPY' ? Number(priceMajor) : Math.round(Number(priceMajor) * 100);

    try {
      await api.createListing({
        name,
        description,
        price: {
          currency_code: currency,
          amount_minor: minor,
        },
        batch_label: batchLabel,
        initial_stock: Number(initialStock),
      });

      setShowAddModal(false);
      setName('');
      setDescription('');
      await loadListings();
    } catch (err: any) {
      setError(err.message || 'Failed to create listing');
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
              <Link to="/seller/dashboard" style={{ color: 'var(--text-secondary)' }}>Seller Hub</Link> / Listings
            </div>
            <h1 style={{ fontSize: '22px', fontWeight: 700, marginTop: '2px' }}>Inventory Listings</h1>
          </div>

          <button
            type="button"
            onClick={() => setShowAddModal(true)}
            className="btn-accent"
            style={{ height: '38px', fontSize: '13px' }}
          >
            <i className="ti ti-plus" /> Add Coffee Listing
          </button>
        </div>

        {/* Modal for Add Listing */}
        {showAddModal && (
          <div style={{
            position: 'fixed',
            inset: 0,
            background: 'rgba(0, 0, 0, 0.4)',
            backdropFilter: 'blur(3px)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 100,
            padding: '20px',
          }}>
            <div style={{
              background: 'var(--surface-1)',
              borderRadius: 'var(--radius-xl)',
              padding: '28px',
              maxWidth: '500px',
              width: '100%',
              boxShadow: 'var(--shadow-lg)',
            }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
                <h2 style={{ fontSize: '18px', fontWeight: 700 }}>Add Coffee Listing</h2>
                <button
                  type="button"
                  onClick={() => setShowAddModal(false)}
                  style={{ background: 'none', border: 'none', fontSize: '18px', cursor: 'pointer' }}
                >
                  ✕
                </button>
              </div>

              {error && (
                <div style={{
                  background: 'var(--badge-danger-bg)',
                  color: 'var(--text-danger)',
                  padding: '10px',
                  borderRadius: 'var(--radius)',
                  fontSize: '12px',
                  marginBottom: '14px',
                }}>
                  {error}
                </div>
              )}

              <form onSubmit={handleCreateListing} style={{ display: 'flex', flexDirection: 'column', gap: '14px' }}>
                <div>
                  <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '4px' }}>
                    Coffee Name *
                  </label>
                  <input
                    type="text"
                    required
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="e.g. Yirgacheffe Light Roast"
                    className="input-control"
                  />
                </div>

                <div>
                  <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '4px' }}>
                    Description
                  </label>
                  <textarea
                    rows={2}
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    placeholder="Tasting notes, processing method, farm origin..."
                    className="input-control"
                    style={{ height: 'auto', padding: '8px 12px' }}
                  />
                </div>

                <div style={{ display: 'grid', gridTemplateColumns: '1fr 2fr', gap: '12px' }}>
                  <div>
                    <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '4px' }}>
                      Currency
                    </label>
                    <select
                      value={currency}
                      onChange={(e) => setCurrency(e.target.value)}
                      className="input-control"
                    >
                      <option value="INR">INR (₹)</option>
                      <option value="USD">USD ($)</option>
                      <option value="GBP">GBP (£)</option>
                      <option value="JPY">JPY (¥)</option>
                    </select>
                  </div>
                  <div>
                    <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '4px' }}>
                      Price *
                    </label>
                    <input
                      type="number"
                      step="any"
                      required
                      value={priceMajor}
                      onChange={(e) => setPriceMajor(e.target.value)}
                      className="input-control"
                    />
                  </div>
                </div>

                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
                  <div>
                    <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '4px' }}>
                      Batch Label
                    </label>
                    <input
                      type="text"
                      value={batchLabel}
                      onChange={(e) => setBatchLabel(e.target.value)}
                      className="input-control"
                    />
                  </div>
                  <div>
                    <label style={{ fontSize: '12px', fontWeight: 600, display: 'block', marginBottom: '4px' }}>
                      Initial Stock (bags) *
                    </label>
                    <input
                      type="number"
                      required
                      value={initialStock}
                      onChange={(e) => setInitialStock(e.target.value)}
                      className="input-control"
                    />
                  </div>
                </div>

                <div style={{ display: 'flex', gap: '10px', marginTop: '10px' }}>
                  <button
                    type="button"
                    onClick={() => setShowAddModal(false)}
                    className="btn-secondary"
                    style={{ flex: 1 }}
                  >
                    Cancel
                  </button>
                  <button
                    type="submit"
                    disabled={submitting}
                    className="btn-accent"
                    style={{ flex: 2 }}
                  >
                    {submitting ? 'Creating...' : 'Save & Publish Listing'}
                  </button>
                </div>
              </form>
            </div>
          </div>
        )}

        {/* Listings Table matching seller_listings_wireframe.html */}
        <div style={{
          background: 'var(--surface-1)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-xl)',
          overflow: 'hidden',
        }}>
          {loading ? (
            <div style={{ textAlign: 'center', padding: '60px 0', color: 'var(--text-muted)' }}>
              <i className="ti ti-loader-2 ti-spin" style={{ fontSize: '24px' }} />
            </div>
          ) : listings.length === 0 ? (
            <div style={{ padding: '48px 20px', textAlign: 'center' }}>
              <div style={{ fontSize: '32px', marginBottom: '12px' }}>📦</div>
              <h3 style={{ fontSize: '16px', fontWeight: 600, marginBottom: '6px' }}>No listings created</h3>
              <p style={{ fontSize: '13px', color: 'var(--text-muted)', marginBottom: '16px' }}>
                Add your coffee offerings to make them discoverable to buyers worldwide.
              </p>
              <button
                type="button"
                onClick={() => setShowAddModal(true)}
                className="btn-accent"
              >
                Add Your First Coffee
              </button>
            </div>
          ) : (
            <table style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left', fontSize: '13px' }}>
              <thead>
                <tr style={{ background: 'var(--surface-2)', borderBottom: '1px solid var(--border)' }}>
                  <th style={{ padding: '12px 16px', fontWeight: 600 }}>Coffee</th>
                  <th style={{ padding: '12px 16px', fontWeight: 600 }}>Batch</th>
                  <th style={{ padding: '12px 16px', fontWeight: 600 }}>Price</th>
                  <th style={{ padding: '12px 16px', fontWeight: 600 }}>Available Stock</th>
                  <th style={{ padding: '12px 16px', fontWeight: 600 }}>Status</th>
                  <th style={{ padding: '12px 16px', fontWeight: 600, textAlign: 'right' }}>Action</th>
                </tr>
              </thead>
              <tbody>
                {listings.map((item) => (
                  <tr key={item.id} style={{ borderBottom: '1px solid var(--border)' }}>
                    <td style={{ padding: '14px 16px' }}>
                      <Link to={`/products/${item.id}`} style={{ fontWeight: 600, color: 'var(--text-primary)' }}>
                        {item.name}
                      </Link>
                    </td>
                    <td style={{ padding: '14px 16px', color: 'var(--text-secondary)' }}>
                      {item.batch_label || '—'}
                    </td>
                    <td style={{ padding: '14px 16px', fontWeight: 600 }}>
                      {formatMoney(item.price)}
                    </td>
                    <td style={{ padding: '14px 16px' }}>
                      {item.stock_available} bags
                    </td>
                    <td style={{ padding: '14px 16px' }}>
                      <span className={`badge ${
                        item.stock_available <= 0
                          ? 'badge-danger'
                          : item.stock_available <= 5
                          ? 'badge-warning'
                          : 'badge-success'
                      }`}>
                        {item.status.replace('PRODUCT_STATUS_', '')}
                      </span>
                    </td>
                    <td style={{ padding: '14px 16px', textAlign: 'right' }}>
                      <Link to={`/products/${item.id}`} style={{ color: 'var(--text-accent)', fontWeight: 600 }}>
                        View →
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </main>
    </div>
  );
};
