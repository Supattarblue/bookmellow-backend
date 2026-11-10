-- =========================================
-- BookMellow Database Schema
-- =========================================

-- 1. USERS: ข้อมูลสมาชิกและผู้ดูแลระบบ
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(100) NOT NULL,
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role VARCHAR(20) NOT NULL DEFAULT 'member'
        CHECK (role IN ('member', 'admin')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 2. BOOKS: ข้อมูลหนังสือ
CREATE TABLE IF NOT EXISTS books (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title VARCHAR(255) NOT NULL,
    author VARCHAR(255) NOT NULL,
    description TEXT,
    cover_url TEXT,
    published_year INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 3. CATEGORIES: หมวดหมู่หนังสือ
CREATE TABLE IF NOT EXISTS categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL UNIQUE
);

-- 4. BOOK_CATEGORIES: ความสัมพันธ์หนังสือกับหมวดหมู่
CREATE TABLE IF NOT EXISTS book_categories (
    book_id UUID NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    category_id UUID NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    PRIMARY KEY (book_id, category_id)
);

-- 5. USER_PREFERENCES: หมวดหมู่ที่สมาชิกสนใจ
CREATE TABLE IF NOT EXISTS user_preferences (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category_id UUID NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, category_id)
);

-- 6. FAVORITES: รายการหนังสือที่สมาชิกชื่นชอบ
CREATE TABLE IF NOT EXISTS favorites (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    book_id UUID NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, book_id)
);

-- 7. REVIEWS: รีวิวและคะแนนหนังสือ
CREATE TABLE IF NOT EXISTS reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    book_id UUID NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    rating INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, book_id)
);

-- =========================================
-- INDEXES: ช่วยให้ค้นหาข้อมูลได้เร็วขึ้น
-- =========================================

CREATE INDEX IF NOT EXISTS idx_books_title
    ON books(title);

CREATE INDEX IF NOT EXISTS idx_reviews_book_id
    ON reviews(book_id);

CREATE INDEX IF NOT EXISTS idx_favorites_book_id
    ON favorites(book_id);

CREATE INDEX IF NOT EXISTS idx_user_preferences_category
    ON user_preferences(category_id);