CREATE TABLE customers (
    customerNumber INT NOT NULL PRIMARY KEY,
    customerName VARCHAR(50) NOT NULL,
    contactLastName VARCHAR(50) NOT NULL,
    contactFirstName VARCHAR(50) NOT NULL,
    phone VARCHAR(50) NOT NULL,
    addressLine1 VARCHAR(50) NOT NULL,
    addressLine2 VARCHAR(50) NULL,
    city VARCHAR(50) NOT NULL,
    state VARCHAR(50) NULL,
    postalCode VARCHAR(15) NULL,
    country VARCHAR(50) NOT NULL,
    salesRepEmployeeNumber INT NULL,
    creditLimit DECIMAL(10,2) NULL,
    CONSTRAINT fk_customers_salesrep FOREIGN KEY (salesRepEmployeeNumber) REFERENCES employees (employeeNumber)
) ENGINE=InnoDB;
