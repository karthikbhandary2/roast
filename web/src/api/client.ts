import { Money } from '../types';

const API_BASE = import.meta.env.VITE_API_URL || '/v1';

export class ApiError extends Error {
  code: string;
  correlationId?: string;

  constructor(message: string, code: string = 'Unknown', correlationId?: string) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.correlationId = correlationId;
  }
}

export function formatMoney(money?: Money): string {
  if (!money) return '$0.00';
  const minor = Number(money.amount_minor || 0);
  const currency = (money.currency_code || '').replace('CURRENCY_CODE_', '') || 'USD';
  
  if (currency === 'JPY') {
    return `¥${minor.toLocaleString()}`;
  }
  const major = (minor / 100).toFixed(2);
  switch (currency) {
    case 'INR':
      return `₹${major}`;
    case 'GBP':
      return `£${major}`;
    case 'EUR':
      return `€${major}`;
    case 'USD':
    default:
      return `$${major}`;
  }
}

export async function request<T>(
  path: string,
  options: RequestInit & { idempotencyKey?: string } = {}
): Promise<T> {
  const token = localStorage.getItem('token');
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string>),
  };

  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  if (options.idempotencyKey) {
    headers['Idempotency-Key'] = options.idempotencyKey;
  }

  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers,
  });

  if (res.status === 204) {
    return {} as T;
  }

  let data: any = null;
  const contentType = res.headers.get('content-type');
  if (contentType && contentType.includes('application/json')) {
    data = await res.json();
  }

  if (!res.ok) {
    const errorMsg = data?.error?.message || `Request failed with status ${res.status}`;
    const errorCode = data?.error?.code || 'Error';
    const correlationId = data?.error?.correlation_id;
    throw new ApiError(errorMsg, errorCode, correlationId);
  }

  return data?.data !== undefined ? data.data : data;
}

export const api = {
  // Auth
  register: (body: any) => request<any>('/auth/register', { method: 'POST', body: JSON.stringify(body) }),
  login: (body: any) => request<any>('/auth/login', { method: 'POST', body: JSON.stringify(body) }),
  logout: () => request<any>('/auth/logout', { method: 'POST' }),

  // Catalog & Search
  getProducts: (params?: { page_size?: number; page_token?: string }) => {
    const query = new URLSearchParams(params as any).toString();
    return request<any[]>(`/products${query ? `?${query}` : ''}`);
  },
  getProduct: (id: string) => request<any>(`/products/${id}`),
  search: (q: string, filter?: string) => request<any>(`/search?q=${encodeURIComponent(q)}${filter ? `&filter=${encodeURIComponent(filter)}` : ''}`),

  // Cart
  getCart: () => request<any>('/cart'),
  addToCart: (productId: string, quantity: number) =>
    request<any>('/cart/items', {
      method: 'POST',
      body: JSON.stringify({ product_id: productId, quantity }),
    }),
  updateCartItem: (productId: string, quantity: number) =>
    request<any>(`/cart/items/${productId}`, {
      method: 'PUT',
      body: JSON.stringify({ quantity }),
    }),

  // Checkout & Orders
  checkout: (body: any, idempotencyKey: string) =>
    request<any>('/checkout', {
      method: 'POST',
      body: JSON.stringify(body),
      idempotencyKey,
    }),
  getPaymentConfig: () => request<{ razorpay_key_id: string }>('/payments/config'),
  createRazorpayOrder: (params: { amount: number; currency: string; receipt?: string; notes?: any }) =>
    request<any>('/payments/razorpay/order', {
      method: 'POST',
      body: JSON.stringify(params),
    }),
  initiatePhonePe: (params: { amount: number; order_id: string; sub_order_id: string }) =>
    request<{ redirect_url: string; merchant_transaction_id: string; status: string }>('/payments/phonepe/initiate', {
      method: 'POST',
      body: JSON.stringify(params),
    }),
  simulateUPI: (subOrderId: string, action: 'approve' | 'decline' = 'approve') =>
    request<any>('/payments/simulate-upi', {
      method: 'POST',
      body: JSON.stringify({ sub_order_id: subOrderId, action }),
    }),
  getOrders: () => request<any[]>('/orders'),
  getOrder: (id: string) => request<any>(`/orders/${id}`),
  confirmDelivery: (orderId: string, subOrderId: string) =>
    request<any>(`/orders/${orderId}/sub-orders/${subOrderId}/confirm-delivery`, { method: 'POST' }),

  // Reviews
  getReviews: (productId: string) => request<any[]>(`/products/${productId}/reviews`),
  createReview: (productId: string, rating: number, comment: string) =>
    request<any>('/reviews', {
      method: 'POST',
      body: JSON.stringify({ product_id: productId, rating, comment }),
    }),

  // Seller
  onboardSeller: (body: any) => request<any>('/seller/onboarding', { method: 'POST', body: JSON.stringify(body) }),
  getSellerDashboard: () => request<any>('/seller/dashboard'),
  getSellerListings: () => request<any[]>('/seller/listings'),
  createListing: (body: any) => request<any>('/seller/listings', { method: 'POST', body: JSON.stringify(body) }),
  getSellerOrders: () => request<any[]>('/seller/orders'),
  markOrderShipped: (subOrderId: string, trackingNumber: string) =>
    request<any>(`/seller/orders/${subOrderId}/ship`, {
      method: 'POST',
      body: JSON.stringify({ tracking_number: trackingNumber }),
    }),
  getSellerPayouts: () => request<any>('/seller/payouts'),

  // Admin
  getAdminInventory: () => request<any[]>('/admin/inventory'),
  getAdminOrders: () => request<any[]>('/admin/orders'),
  getAdminSellers: (status?: string) =>
    request<any[]>(`/admin/sellers${status ? `?status=${status}` : ''}`),
  reviewSeller: (userId: string, status: string, reason: string) =>
    request<any>(`/admin/sellers/${userId}/review`, {
      method: 'POST',
      body: JSON.stringify({ status, reason }),
    }),
};
