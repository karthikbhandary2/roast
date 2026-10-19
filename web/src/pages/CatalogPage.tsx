import React, { useState, useEffect } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { api, formatMoney } from '../api/client';
import { Product } from '../types';
import { Navbar } from '../components/Navbar';

export const CatalogPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const [products, setProducts] = useState<Product[]>([]);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState(searchParams.get('q') || '');
  const [selectedRoast, setSelectedRoast] = useState<string>('all');
  const [inStockOnly, setInStockOnly] = useState(false);

  useEffect(() => {
    const q = searchParams.get('q') || '';
    setQuery(q);
    loadProducts(q);
  }, [searchParams]);

  const loadProducts = async (searchQuery: string) => {
    setLoading(true);
    try {
      if (searchQuery.trim()) {
        const res = await api.search(searchQuery.trim());
        setProducts(res?.hits || []);
      } else {
        const res = await api.getProducts({ page_size: 50 });
        setProducts(res || []);
      }
    } catch (err) {
      console.error('Failed to load products', err);
      setProducts([]);
    } finally {
      setLoading(false);
    }
  };

  const getProductRoast = (p: Product): string => {
    if (p.roast_level) return p.roast_level.toLowerCase();
    const text = `${p.name} ${p.description || ''}`.toLowerCase();
    if (text.includes('light')) return 'light';
    if (text.includes('medium')) return 'medium';
    if (text.includes('dark')) return 'dark';
    return '';
  };

  const filteredProducts = products.filter((p) => {
    if (inStockOnly && p.stock_available <= 0) return false;
    if (selectedRoast !== 'all') {
      const roast = getProductRoast(p);
      if (!roast || !roast.includes(selectedRoast.toLowerCase())) {
        return false;
      }
    }
    return true;
  });

  return (
    <div>
      <Navbar
        searchQuery={query}
        onSearchChange={(val) => {
          setQuery(val);
          if (!val) {
            searchParams.delete('q');
            setSearchParams(searchParams);
          }
        }}
      />

      <main className="container" style={{ padding: '24px 20px 48px' }}>
        {/* Filter bar matching catalog_search_wireframe.html */}
        <div style={{
          display: 'flex',
          gap: '10px',
          marginBottom: '20px',
          flexWrap: 'wrap',
          alignItems: 'center',
        }}>
          {/* Roast level filter */}
          <select
            value={selectedRoast}
            onChange={(e) => setSelectedRoast(e.target.value)}
            style={{
              fontSize: '13px',
              border: '1px solid var(--border-strong)',
              borderRadius: 'var(--radius-pill)',
              padding: '6px 14px',
              background: 'var(--surface-1)',
              color: 'var(--text-secondary)',
              cursor: 'pointer',
              outline: 'none',
            }}
          >
            <option value="all">Roast level: All</option>
            <option value="light">Light roast</option>
            <option value="medium">Medium roast</option>
            <option value="dark">Dark roast</option>
          </select>

          {/* In stock only toggle */}
          <button
            type="button"
            onClick={() => setInStockOnly(!inStockOnly)}
            style={{
              fontSize: '13px',
              border: `1px solid ${inStockOnly ? 'var(--border-accent)' : 'var(--border-strong)'}`,
              borderRadius: 'var(--radius-pill)',
              padding: '6px 14px',
              background: inStockOnly ? 'var(--badge-warning-bg)' : 'var(--surface-1)',
              color: inStockOnly ? 'var(--text-accent)' : 'var(--text-secondary)',
              cursor: 'pointer',
              fontWeight: inStockOnly ? 600 : 400,
            }}
          >
            {inStockOnly ? '✓ In stock only' : 'In stock only'}
          </button>

          <div style={{ marginLeft: 'auto', fontSize: '13px', color: 'var(--text-muted)' }}>
            {filteredProducts.length} {filteredProducts.length === 1 ? 'coffee' : 'coffees'} available
          </div>
        </div>

        {/* Product Grid */}
        {loading ? (
          <div style={{ textAlign: 'center', padding: '60px 0', color: 'var(--text-muted)' }}>
            <i className="ti ti-loader-2 ti-spin" style={{ fontSize: '24px', marginBottom: '8px', display: 'block' }} />
            Discovering fresh roasts...
          </div>
        ) : filteredProducts.length === 0 ? (
          <div style={{
            background: 'var(--surface-1)',
            border: '1px solid var(--border)',
            borderRadius: 'var(--radius-lg)',
            padding: '48px 20px',
            textAlign: 'center',
          }}>
            <div style={{ fontSize: '32px', marginBottom: '12px' }}>☕</div>
            <h3 style={{ fontSize: '16px', fontWeight: 600, marginBottom: '6px' }}>No coffees found</h3>
            <p style={{ fontSize: '13px', color: 'var(--text-muted)', marginBottom: '16px' }}>
              Try adjusting your search query or filters.
            </p>
            <button
              onClick={() => {
                setQuery('');
                setSelectedRoast('all');
                setInStockOnly(false);
                setSearchParams({});
              }}
              className="btn-secondary"
            >
              Reset Filters
            </button>
          </div>
        ) : (
          <div style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))',
            gap: '16px',
          }}>
            {filteredProducts.map((p) => {
              const isSoldOut = p.stock_available <= 0 || p.status === 'PRODUCT_STATUS_SOLD_OUT';
              const isLowStock = p.stock_available > 0 && p.stock_available <= 5;

              return (
                <Link
                  key={p.id}
                  to={`/products/${p.id}`}
                  style={{
                    background: 'var(--surface-1)',
                    border: '1px solid var(--border)',
                    borderRadius: 'var(--radius-lg)',
                    padding: '12px',
                    transition: 'transform 0.15s ease, box-shadow 0.15s ease',
                    display: 'flex',
                    flexDirection: 'column',
                  }}
                  onMouseEnter={(e) => {
                    e.currentTarget.style.transform = 'translateY(-2px)';
                    e.currentTarget.style.boxShadow = 'var(--shadow-md)';
                  }}
                  onMouseLeave={(e) => {
                    e.currentTarget.style.transform = 'translateY(0)';
                    e.currentTarget.style.boxShadow = 'none';
                  }}
                >
                  {/* Image placeholder / mockup */}
                  <div style={{
                    background: 'var(--surface-2)',
                    borderRadius: 'var(--radius)',
                    height: '140px',
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'center',
                    justifyContent: 'center',
                    color: 'var(--text-muted)',
                    fontSize: '12px',
                    marginBottom: '10px',
                    position: 'relative',
                    overflow: 'hidden',
                  }}>
                    <span style={{ fontSize: '36px', opacity: 0.8 }}>🫘</span>
                    <span style={{ fontSize: '11px', marginTop: '4px', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                      {p.batch_label || 'Single Origin'}
                    </span>
                  </div>

                  <h3 style={{ fontSize: '14px', fontWeight: 600, color: 'var(--text-primary)', marginBottom: '2px' }}>
                    {p.name}
                  </h3>

                  <div style={{ fontSize: '12px', color: 'var(--text-secondary)', marginBottom: '8px' }}>
                    {p.origin || 'Specialty Blend'} · {p.roast_level || (getProductRoast(p) ? `${getProductRoast(p).charAt(0).toUpperCase() + getProductRoast(p).slice(1)} Roast` : 'Medium Roast')}
                  </div>

                  <div style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    marginTop: 'auto',
                    paddingTop: '8px',
                    borderTop: '0.5px solid var(--border)',
                  }}>
                    <span style={{ fontSize: '15px', fontWeight: 700, color: 'var(--text-primary)' }}>
                      {formatMoney(p.price)}
                    </span>

                    {isSoldOut ? (
                      <span className="badge badge-danger">Sold out</span>
                    ) : isLowStock ? (
                      <span className="badge badge-warning">{p.stock_available} bags left</span>
                    ) : (
                      <span className="badge badge-success">{p.stock_available} in stock</span>
                    )}
                  </div>
                </Link>
              );
            })}
          </div>
        )}
      </main>
    </div>
  );
};
