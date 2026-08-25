CREATE TABLE orders (
    orderNumber INT NOT NULL PRIMARY KEY,
    orderDate DATETIME NOT NULL,
    requiredDate DATETIME NOT NULL,
    shippedDate DATETIME NULL,
    status VARCHAR(15) NOT NULL,
    comments TEXT NULL,
    customerNumber INT NOT NULL,
    CONSTRAINT fk_orders_customer FOREIGN KEY (customerNumber) REFERENCES customers (customerNumber)
) ENGINE=InnoDB;

CREATE TABLE orderdetails (
    orderNumber INT NOT NULL,
    productCode VARCHAR(15) NOT NULL,
    quantityOrdered INT NOT NULL,
    priceEach DECIMAL(10,2) NOT NULL,
    orderLineNumber SMALLINT NOT NULL,
    PRIMARY KEY (orderNumber, productCode),
    CONSTRAINT fk_orderdetails_order FOREIGN KEY (orderNumber) REFERENCES orders (orderNumber),
    CONSTRAINT fk_orderdetails_product FOREIGN KEY (productCode) REFERENCES products (productCode)
) ENGINE=InnoDB;

CREATE TABLE payments (
    customerNumber INT NOT NULL,
    checkNumber VARCHAR(50) NOT NULL,
    paymentDate DATETIME NOT NULL,
    amount DECIMAL(10,2) NOT NULL,
    PRIMARY KEY (customerNumber, checkNumber),
    CONSTRAINT fk_payments_customer FOREIGN KEY (customerNumber) REFERENCES customers (customerNumber)
) ENGINE=InnoDB;
