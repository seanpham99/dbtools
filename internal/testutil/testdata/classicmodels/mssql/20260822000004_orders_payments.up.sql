CREATE TABLE orders (
    orderNumber INT NOT NULL PRIMARY KEY,
    orderDate DATETIME NOT NULL,
    requiredDate DATETIME NOT NULL,
    shippedDate DATETIME NULL,
    status VARCHAR(15) NOT NULL CHECK (status IN ('Shipped', 'Resolved', 'Cancelled', 'On Hold', 'Disputed', 'In Process')),
    comments VARCHAR(MAX) NULL,
    customerNumber INT NOT NULL FOREIGN KEY REFERENCES customers(customerNumber)
);
GO
CREATE TABLE orderdetails (
    orderNumber INT NOT NULL FOREIGN KEY REFERENCES orders(orderNumber),
    productCode VARCHAR(15) NOT NULL FOREIGN KEY REFERENCES products(productCode),
    quantityOrdered INT NOT NULL,
    priceEach DECIMAL(10,2) NOT NULL,
    orderLineNumber SMALLINT NOT NULL,
    PRIMARY KEY (orderNumber, productCode)
);
GO
CREATE TABLE payments (
    customerNumber INT NOT NULL FOREIGN KEY REFERENCES customers(customerNumber),
    checkNumber VARCHAR(50) NOT NULL,
    paymentDate DATETIME NOT NULL,
    amount DECIMAL(10,2) NOT NULL,
    PRIMARY KEY (customerNumber, checkNumber)
);
