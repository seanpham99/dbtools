CREATE TABLE productlines (
    productLine VARCHAR(50) NOT NULL PRIMARY KEY,
    textDescription VARCHAR(4000) NULL,
    htmlDescription MEDIUMTEXT NULL,
    image MEDIUMBLOB NULL
) ENGINE=InnoDB;

CREATE TABLE products (
    productCode VARCHAR(15) NOT NULL PRIMARY KEY,
    productName VARCHAR(70) NOT NULL,
    productLine VARCHAR(50) NOT NULL,
    productScale VARCHAR(10) NOT NULL,
    productVendor VARCHAR(50) NOT NULL,
    productDescription TEXT NOT NULL,
    quantityInStock INT NOT NULL,
    buyPrice DECIMAL(10,2) NOT NULL,
    MSRP DECIMAL(10,2) NOT NULL,
    CONSTRAINT fk_products_productline FOREIGN KEY (productLine) REFERENCES productlines (productLine)
) ENGINE=InnoDB;
