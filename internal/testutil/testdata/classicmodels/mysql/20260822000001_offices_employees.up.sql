CREATE TABLE offices (
    officeCode VARCHAR(10) NOT NULL PRIMARY KEY,
    city VARCHAR(50) NOT NULL,
    phone VARCHAR(50) NOT NULL,
    addressLine1 VARCHAR(50) NOT NULL,
    addressLine2 VARCHAR(50) NULL,
    state VARCHAR(50) NULL,
    country VARCHAR(50) NOT NULL,
    postalCode VARCHAR(15) NOT NULL,
    territory VARCHAR(10) NOT NULL
) ENGINE=InnoDB;

CREATE TABLE employees (
    employeeNumber INT NOT NULL PRIMARY KEY,
    lastName VARCHAR(50) NOT NULL,
    firstName VARCHAR(50) NOT NULL,
    extension VARCHAR(10) NOT NULL,
    email VARCHAR(100) NOT NULL,
    officeCode VARCHAR(10) NOT NULL,
    reportsTo INT NULL,
    jobTitle VARCHAR(50) NOT NULL,
    CONSTRAINT fk_employees_office FOREIGN KEY (officeCode) REFERENCES offices (officeCode),
    CONSTRAINT fk_employees_reportsto FOREIGN KEY (reportsTo) REFERENCES employees (employeeNumber)
) ENGINE=InnoDB;
