export interface Money {
  currency_code: string;
  amount_minor: string | number;
}

export interface User {
  id: string;
  email: string;
  display_name: string;
  roles: string[];
}

export interface AuthResponse {
  access_token: string;
  expires_in_seconds: number;
}

export interface Product {
  id: string;
  seller_id: string;
  name: string;
  description: string;
  origin?: string;
  roast_level?: string;
  process?: string;
  flavor_notes?: string[];
  price: Money;
  stock_available: number;
  status: string; // "PRODUCT_STATUS_LIVE" | "PRODUCT_STATUS_LOW_STOCK" | "PRODUCT_STATUS_SOLD_OUT"
  batch_label?: string;
  image_urls?: string[];
  updated_at?: string;
}

export interface CartItem {
  product_id: string;
  seller_id: string;
  quantity: number;
  unit_price: Money;
  product?: Product;
}

export interface Cart {
  buyer_id: string;
  items: CartItem[];
  expires_at?: string;
}

export interface ShippingAddress {
  full_name: string;
  line1: string;
  line2?: string;
  city: string;
  postal_code: string;
  country_code: string;
  phone: string;
}

export interface SubOrder {
  id: string;
  parent_order_id: string;
  seller_id: string;
  items: {
    product_id: string;
    seller_id: string;
    quantity: number;
    unit_price: Money;
  }[];
  subtotal: Money;
  status: string; // "SUB_ORDER_STATUS_CONFIRMED" | "SUB_ORDER_STATUS_SHIPPED" | "SUB_ORDER_STATUS_DELIVERED"
  reservation_id?: string;
  charge_id?: string;
  tracking_number?: string;
  failure_reason?: string;
}

export interface Order {
  id: string;
  buyer_id: string;
  shipping_address: ShippingAddress;
  sub_orders: SubOrder[];
  total: Money;
  status: string; // "ORDER_STATUS_CONFIRMED" | "ORDER_STATUS_PROCESSING" | "ORDER_STATUS_FAILED"
  created_at: string;
}

export interface Review {
  id: string;
  product_id: string;
  buyer_id: string;
  sub_order_id: string;
  rating: number;
  comment: string;
  created_at: string;
}

export interface SellerProfile {
  user_id: string;
  business_name: string;
  provider: string; // "PAYMENT_PROVIDER_RAZORPAY" | "PAYMENT_PROVIDER_STRIPE"
  payout_method: string;
  payout_account_ref: string;
  commission_rate: number;
  status: string; // "SELLER_STATUS_PENDING_REVIEW" | "SELLER_STATUS_ACTIVE" | "SELLER_STATUS_SUSPENDED" | "SELLER_STATUS_REJECTED"
}

export interface SellerDashboard {
  pending_payout?: Money;
  available_payout?: Money;
  orders_awaiting_fulfillment: number;
  listings_low_on_stock: number;
}

export interface Payout {
  id: string;
  seller_id: string;
  amount: Money;
  status: string;
  provider_payout_id?: string;
  created_at: string;
}

export interface SagaEvent {
  event_type: string;
  sub_order_id?: string;
  parent_order_id: string;
  status: string;
  message?: string;
  timestamp: string;
}
