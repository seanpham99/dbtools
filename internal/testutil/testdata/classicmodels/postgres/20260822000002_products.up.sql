CREATE TABLE productlines (
    productLine VARCHAR(50) NOT NULL PRIMARY KEY,
    textDescription VARCHAR(4000) DEFAULT NULL,
    htmlDescription TEXT DEFAULT NULL,
    image BYTEA DEFAULT NULL
);

CREATE TABLE products (
    productCode VARCHAR(15) NOT NULL PRIMARY KEY,
    productName VARCHAR(70) NOT NULL,
    productLine VARCHAR(50) NOT NULL REFERENCES productlines(productLine),
    productScale VARCHAR(10) NOT NULL,
    productVendor VARCHAR(50) NOT NULL,
    productDescription TEXT NOT NULL,
    quantityInStock SMALLINT NOT NULL,
    buyPrice NUMERIC(10,2) NOT NULL,
    MSRP NUMERIC(10,2) NOT NULL
);
