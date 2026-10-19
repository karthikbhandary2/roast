import React, { useState, useEffect } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import { api, formatMoney } from '../api/client';
import { Product, Review } from '../types';
import { Navbar } from '../components/Navbar';
import { useAuth } from '../hooks/useAuth';

export const ProductDetailPage: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const { user } = useAuth();
  const navigate = useNavigate();

  const [product, setProduct] = useState<Product | null>(null);
  const [reviews, setReviews] = useState<Review[]>([]);
  const [quantity, setQuantity] = useState(1);
  const [loading, setLoading] = useState(true);
  const [addingToCart, setAddingToCart] = useState(false);
  const [addedSuccess, setAddedSuccess] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Review form
  const [rating, setRating] = useState(5);
  const [comment, setComment] = useState('');
  const [reviewSubmitting, setReviewSubmitting] = useState(false);
  const [reviewError, setReviewError] = useState<string | null>(null);
  const [reviewSuccess, setReviewSuccess] = useState(false);

  useEffect(() => {
    if (id) {
      loadData(id);
    }
  }, [id]);

  const loadData = async (productId: string) => {
    setLoading(true);
    try {
      const p = await api.getProduct(productId);
      setProduct(p);
      const revs = await api.getReviews(productId).catch(() => []);
      setReviews(revs || []);
    } catch (err: any) {
      setError(err.message || 'Product not found');
    } finally {
      setLoading(false);
    }
  };

  const handleAddToCart = async () => {
    if (!user) {
      navigate('/login');
      return;
    }
    if (!product) return;

    setAddingToCart(true);
    try {
      await api.addToCart(product.id, quantity);
      setAddedSuccess(true);
      window.dispatchEvent(new CustomEvent('cart-updated'));
      setTimeout(() => setAddedSuccess(false), 2500);
    } catch (err: any) {
      alert(`Could not add to cart: ${err.message}`);
    } finally {
      setAddingToCart(false);
    }
  };

  const handleReviewSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!user) {
      navigate('/login');
      return;
    }
    if (!id || !comment.trim()) return;

    setReviewSubmitting(true);
    setReviewError(null);
    try {
      const rev = await api.createReview(id, rating, comment);
      setReviews([rev, ...reviews]);
      setComment('');
      setReviewSuccess(true);
      setTimeout(() => setReviewSuccess(false), 3000);
    } catch (err: any) {
      setReviewError(err.message || 'Failed to submit review');
    } finally {
      setReviewSubmitting(false);
    }
  };

  if (loading) {
    return (
      <div>
        <Navbar />
        <div style={{ textAlign: 'center', padding: '80px 0', color: 'var(--text-muted)' }}>
          <i className="ti ti-loader-2 ti-spin" style={{ fontSize: '28px', display: 'block', marginBottom: '8px' }} />
          Loading coffee details...
        </div>
      </div>
    );
  }

  if (error || !product) {
    return (
      <div>
        <Navbar />
        <div className="container" style={{ padding: '60px 20px', textAlign: 'center' }}>
          <h2>Coffee Not Found</h2>
          <p style={{ color: 'var(--text-muted)', margin: '12px 0 20px' }}>{error}</p>
          <Link to="/" className="btn-secondary">Return to Catalog</Link>
        </div>
      </div>
    );
  }

  const isSoldOut = product.stock_available <= 0;

  return (
    <div>
      <Navbar />

      <main className="container" style={{ padding: '24px 20px 60px' }}>
        <Link to="/" style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: '6px',
          fontSize: '13px',
          color: 'var(--text-secondary)',
          marginBottom: '20px',
        }}>
          <i className="ti ti-arrow-left" /> Back to catalog
        </Link>

        {/* 2-Column layout matching product_detail_wireframe.html */}
        <div style={{
          display: 'grid',
          gridTemplateColumns: 'minmax(0, 1.2fr) minmax(0, 1fr)',
          gap: '32px',
          background: 'var(--surface-1)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-xl)',
          padding: '32px',
          marginBottom: '40px',
        }}>
          {/* Left: Image / Roaster Details */}
          <div>
            <div style={{
              background: 'var(--surface-2)',
              borderRadius: 'var(--radius-lg)',
              height: '320px',
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              justifyContent: 'center',
              border: '1px solid var(--border)',
              marginBottom: '20px',
            }}>
              <span style={{ fontSize: '64px', marginBottom: '8px' }}>☕</span>
              <span style={{ fontSize: '13px', fontWeight: 600, color: 'var(--text-secondary)', letterSpacing: '0.05em' }}>
                BATCH: {product.batch_label || 'ORIGIN ROAST'}
              </span>
            </div>

            <h3 style={{ fontSize: '15px', fontWeight: 600, marginBottom: '8px' }}>About this Roast</h3>
            <p style={{ fontSize: '13px', color: 'var(--text-secondary)', lineHeight: '1.6', marginBottom: '16px' }}>
              {product.description || 'Specialty single origin harvested with care and freshly roasted to preserve unique terroir flavor notes.'}
            </p>

            <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
              {product.origin && (
                <span className="badge badge-neutral">📍 {product.origin}</span>
              )}
              {product.roast_level && (
                <span className="badge badge-neutral">🔥 {product.roast_level}</span>
              )}
              {product.process && (
                <span className="badge badge-neutral">💧 {product.process}</span>
              )}
            </div>
          </div>

          {/* Right: Price, Stock, Add to Cart */}
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            <div style={{ marginBottom: '16px' }}>
              <span style={{ fontSize: '12px', fontWeight: 600, textTransform: 'uppercase', color: 'var(--text-accent)' }}>
                Specialty Coffee
              </span>
              <h1 style={{ fontSize: '24px', fontWeight: 700, marginTop: '4px', marginBottom: '8px' }}>
                {product.name}
              </h1>
              <div style={{ fontSize: '24px', fontWeight: 700, color: 'var(--text-primary)' }}>
                {formatMoney(product.price)}
              </div>
            </div>

            <div style={{
              background: 'var(--surface-2)',
              borderRadius: 'var(--radius)',
              padding: '16px',
              border: '1px solid var(--border)',
              marginBottom: '24px',
            }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '12px' }}>
                <span style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>Availability</span>
                {isSoldOut ? (
                  <span className="badge badge-danger">Sold Out</span>
                ) : (
                  <span className="badge badge-success">{product.stock_available} bags available</span>
                )}
              </div>

              {/* Quantity selector */}
              {!isSoldOut && (
                <div style={{ display: 'flex', alignItems: 'center', gap: '12px', marginBottom: '16px' }}>
                  <span style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>Quantity:</span>
                  <div style={{
                    display: 'flex',
                    alignItems: 'center',
                    border: '1px solid var(--border-strong)',
                    borderRadius: 'var(--radius)',
                    background: 'var(--surface-1)',
                  }}>
                    <button
                      type="button"
                      onClick={() => setQuantity(Math.max(1, quantity - 1))}
                      style={{ border: 'none', background: 'none', padding: '6px 12px', fontSize: '14px' }}
                    >
                      -
                    </button>
                    <span style={{ minWidth: '28px', textAlign: 'center', fontSize: '14px', fontWeight: 600 }}>
                      {quantity}
                    </span>
                    <button
                      type="button"
                      onClick={() => setQuantity(Math.min(product.stock_available, quantity + 1))}
                      style={{ border: 'none', background: 'none', padding: '6px 12px', fontSize: '14px' }}
                    >
                      +
                    </button>
                  </div>
                </div>
              )}

              <button
                type="button"
                onClick={handleAddToCart}
                disabled={isSoldOut || addingToCart}
                className={isSoldOut ? 'btn-secondary' : 'btn-accent'}
                style={{
                  width: '100%',
                  height: '44px',
                  fontSize: '14px',
                  fontWeight: 600,
                  opacity: isSoldOut ? 0.6 : 1,
                  cursor: isSoldOut ? 'not-allowed' : 'pointer',
                }}
              >
                {addedSuccess ? (
                  <>✓ Added to Cart!</>
                ) : addingToCart ? (
                  <><i className="ti ti-loader-2 ti-spin" /> Adding...</>
                ) : isSoldOut ? (
                  'Out of Stock'
                ) : (
                  <><i className="ti ti-shopping-cart" /> Add to Cart</>
                )}
              </button>
            </div>

            <div style={{ fontSize: '12px', color: 'var(--text-muted)', display: 'flex', gap: '16px' }}>
              <span>🚚 Ships fresh within 24 hours</span>
              <span>🔒 Sealed freshness guarantee</span>
            </div>
          </div>
        </div>

        {/* Reviews Section */}
        <div style={{
          background: 'var(--surface-1)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius-xl)',
          padding: '32px',
        }}>
          <h2 style={{ fontSize: '18px', fontWeight: 700, marginBottom: '20px' }}>
            Customer Reviews ({reviews.length})
          </h2>

          {/* Write a review form */}
          {user && (
            <form onSubmit={handleReviewSubmit} style={{
              background: 'var(--surface-2)',
              borderRadius: 'var(--radius-lg)',
              padding: '20px',
              border: '1px solid var(--border)',
              marginBottom: '28px',
            }}>
              <h3 style={{ fontSize: '14px', fontWeight: 600, marginBottom: '10px' }}>
                Leave a Verified Review
              </h3>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '12px' }}>
                <span style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>Rating:</span>
                {[1, 2, 3, 4, 5].map((star) => (
                  <button
                    key={star}
                    type="button"
                    onClick={() => setRating(star)}
                    style={{
                      background: 'none',
                      border: 'none',
                      fontSize: '18px',
                      color: star <= rating ? '#e8a317' : 'var(--border-strong)',
                      cursor: 'pointer',
                    }}
                  >
                    ★
                  </button>
                ))}
              </div>

              <textarea
                placeholder="Share your tasting notes, brew method, and experience..."
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                required
                rows={3}
                className="input-control"
                style={{ height: 'auto', padding: '10px 12px', resize: 'vertical', marginBottom: '10px' }}
              />

              {reviewError && (
                <div style={{ fontSize: '12px', color: 'var(--text-danger)', marginBottom: '10px' }}>
                  {reviewError.includes('verified purchase') || reviewError.includes('delivered')
                    ? 'Note: Only buyers with a delivered order for this coffee can leave a review.'
                    : reviewError}
                </div>
              )}

              {reviewSuccess && (
                <div style={{ fontSize: '12px', color: 'var(--text-success)', marginBottom: '10px' }}>
                  ✓ Review submitted successfully!
                </div>
              )}

              <button
                type="submit"
                disabled={reviewSubmitting || !comment.trim()}
                className="btn-primary"
                style={{ height: '36px', fontSize: '13px' }}
              >
                {reviewSubmitting ? 'Submitting...' : 'Post Review'}
              </button>
            </form>
          )}

          {/* List of reviews */}
          {reviews.length === 0 ? (
            <div style={{ color: 'var(--text-muted)', fontSize: '13px', fontStyle: 'italic' }}>
              No reviews yet. Be the first to brew and review this roast!
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
              {reviews.map((r) => (
                <div key={r.id} style={{
                  paddingBottom: '16px',
                  borderBottom: '1px solid var(--border)',
                }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '4px' }}>
                    <span style={{ color: '#e8a317', fontSize: '14px' }}>
                      {'★'.repeat(r.rating)}{'☆'.repeat(5 - r.rating)}
                    </span>
                    <span className="badge badge-success">✓ Verified Purchase</span>
                    <span style={{ fontSize: '11px', color: 'var(--text-muted)', marginLeft: 'auto' }}>
                      {new Date(r.created_at).toLocaleDateString()}
                    </span>
                  </div>
                  <p style={{ fontSize: '13px', color: 'var(--text-primary)', marginTop: '4px' }}>
                    {r.comment}
                  </p>
                </div>
              ))}
            </div>
          )}
        </div>
      </main>
    </div>
  );
};
