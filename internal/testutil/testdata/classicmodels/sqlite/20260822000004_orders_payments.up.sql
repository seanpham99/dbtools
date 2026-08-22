CREATE TABLE orders (
    orderNumber INTEGER NOT NULL PRIMARY KEY,
    orderDate DATE NOT NULL,
    requiredDate DATE NOT NULL,
    shippedDate DATE DEFAULT NULL,
    status VARCHAR(15) NOT NULL CHECK (status IN ('Shipped', 'Resolved', 'Cancelled', 'On Hold', 'Disputed', 'In Process')),
    comments TEXT DEFAULT NULL,
    customerNumber INTEGER NOT NULL REFERENCES customers(customerNumber)
);

CREATE TABLE orderdetails (
    orderNumber INTEGER NOT NULL REFERENCES orders(orderNumber),
    productCode VARCHAR(15) NOT NULL REFERENCES products(productCode),
    quantityOrdered INTEGER NOT NULL,
    priceEach DECIMAL(10,2) NOT NULL,
    orderLineNumber SMALLINT NOT NULL,
    PRIMARY KEY (orderNumber, productCode)
);

CREATE TABLE payments (
    customerNumber INTEGER NOT NULL REFERENCES customers(customerNumber),
    checkNumber VARCHAR(50) NOT NULL,
    paymentDate DATE NOT NULL,
    amount DECIMAL(10,2) NOT NULL,
    PRIMARY KEY (customerNumber, checkNumber)
);
