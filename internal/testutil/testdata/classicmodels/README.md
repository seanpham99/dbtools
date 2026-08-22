# ClassicModels Fixture Corpus

This directory contains the canonical `classicmodels` relational sample database, ported across SQLite, PostgreSQL, and Microsoft SQL Server.

## Provenance
- Source: MySQL Sample Database `classicmodels` (v3.2) from mysqltutorial.org
- Mirror Reference: https://gist.githubusercontent.com/prof3ssorSt3v3/796ebc82fd8eeb0b697effaa1e86c3a6/raw/classicmodels.sql
- License: Public Domain / Educational Sample

## Tables (8)
1. `offices`: Office locations (PK: `officeCode`)
2. `employees`: Employees hierarchy (PK: `employeeNumber`, FK self-ref: `reportsTo`, FK: `officeCode`)
3. `productlines`: Product categories (PK: `productLine`)
4. `products`: Inventory items (PK: `productCode`, FK: `productLine`)
5. `customers`: Customer accounts (PK: `customerNumber`, FK: `salesRepEmployeeNumber`)
6. `orders`: Customer orders (PK: `orderNumber`, FK: `customerNumber`, status CHECK constraint)
7. `orderdetails`: Order line items (Composite PK: `orderNumber` + `productCode`)
8. `payments`: Payment transactions (Composite PK: `customerNumber` + `checkNumber`)

## Dialect Migration Sequence
Each dialect directory (`sqlite/`, `postgres/`, `mssql/`) provides a 4-step migration sequence with matching `.up.sql` and `.down.sql` scripts:
- `20260822000001_offices_employees`: Core organizational hierarchy
- `20260822000002_products`: Product lines and inventory
- `20260822000003_customers`: Customers linked to sales representatives
- `20260822000004_orders_payments`: Orders, order details, and payment ledgers
