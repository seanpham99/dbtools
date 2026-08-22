-- Shared portable seed data for classicmodels fixture suite
-- Exercises: 8 tables, self-referential FK (reportsTo), composite PKs, NULLs, decimal/money, status enums

INSERT INTO offices (officeCode, city, phone, addressLine1, addressLine2, state, country, postalCode, territory) VALUES
('1', 'San Francisco', '+1 650 219 4782', '100 Market Street', 'Suite 300', 'CA', 'USA', '94080', 'NA'),
('2', 'Boston', '+1 215 837 0825', '1550 Court Place', 'Suite 102', 'MA', 'USA', '02107', 'NA'),
('3', 'NYC', '+1 212 555 3000', '523 East 53rd Street', 'apt. 5A', 'NY', 'USA', '10022', 'NA'),
('4', 'Paris', '+33 14 723 4404', '43 Rue Jouffroy D''abbans', NULL, NULL, 'France', '75017', 'EMEA'),
('5', 'Tokyo', '+81 33 224 5000', '4-1 Kioicho', NULL, 'Chiyoda-Ku', 'Japan', '102-8578', 'Japan'),
('6', 'Sydney', '+61 2 9264 2451', '5-11 Wentworth Avenue', 'Floor #2', NULL, 'Australia', 'NSW 2010', 'APAC'),
('7', 'London', '+44 20 7877 2041', '25 Old Broad Street', 'Level 7', NULL, 'UK', 'EC2N 1HN', 'EMEA');

INSERT INTO employees (employeeNumber, lastName, firstName, extension, email, officeCode, reportsTo, jobTitle) VALUES
(1002, 'Murphy', 'Diane', 'x5800', 'dmurphy@classicmodelcars.com', '1', NULL, 'President'),
(1056, 'Patterson', 'Mary', 'x4611', 'mpatterso@classicmodelcars.com', '1', 1002, 'VP Sales'),
(1076, 'Firrelli', 'Jeff', 'x9273', 'jfirrelli@classicmodelcars.com', '1', 1002, 'VP Marketing'),
(1102, 'Bondur', 'Gerard', 'x5408', 'gbondur@classicmodelcars.com', '4', 1056, 'Sale Manager (EMEA)'),
(1143, 'Bow', 'Anthony', 'x5428', 'abow@classicmodelcars.com', '1', 1056, 'Sales Manager (NA)'),
(1165, 'Jennings', 'Leslie', 'x3291', 'ljennings@classicmodelcars.com', '1', 1143, 'Sales Rep'),
(1166, 'Thompson', 'Leslie', 'x4065', 'lthompson@classicmodelcars.com', '1', 1143, 'Sales Rep'),
(1370, 'Hernandez', 'Gerard', 'x2028', 'ghernande@classicmodelcars.com', '4', 1102, 'Sales Rep'),
(1501, 'Bott', 'Larry', 'x2311', 'lbott@classicmodelcars.com', '7', 1102, 'Sales Rep'),
(1611, 'Fixter', 'Andy', 'x0101', 'afixter@classicmodelcars.com', '6', 1056, 'Sales Rep');

INSERT INTO productlines (productLine, textDescription, htmlDescription, image) VALUES
('Classic Cars', 'Attention car enthusiasts: vintage diecast cars from the 1950s to 1980s.', NULL, NULL),
('Motorcycles', 'Scale replicas of classic motorbikes and racing bikes.', NULL, NULL),
('Planes', 'Detailed military and civilian aircraft models.', NULL, NULL),
('Ships', 'Historical wooden and steel naval vessels.', NULL, NULL),
('Trains', 'Authentic steam and electric locomotive models.', NULL, NULL),
('Trucks and Buses', 'Heavy transport, fire trucks, and passenger buses.', NULL, NULL),
('Vintage Cars', 'Pre-war motor carriages and antique roadsters.', NULL, NULL);

INSERT INTO products (productCode, productName, productLine, productScale, productVendor, productDescription, quantityInStock, buyPrice, MSRP) VALUES
('S10_1678', '1969 Harley Davidson Ultimate Chopper', 'Motorcycles', '1:10', 'Min Lin Diecast', 'This replica features chrome pipes, working suspension and rubber tires.', 7933, 48.81, 95.70),
('S10_1949', '1952 Alpine Renault 1300', 'Classic Cars', '1:10', 'Classic Metal Creations', 'Turnable front wheels; detailed interior; opening hood; opening trunk.', 7305, 98.58, 214.30),
('S10_2016', '1996 Moto Guzzi 1100i', 'Motorcycles', '1:10', 'Highway 66 Mini Classics', 'Official licensed model with detailed V-twin engine and disk brakes.', 6625, 68.99, 118.94),
('S12_1099', '1968 Ford Mustang', 'Classic Cars', '1:12', 'Autoart Studio Design', 'Detailed 390 cu in V8 engine, opening doors, authentic interior.', 68, 95.34, 194.57),
('S18_1749', '1917 Grand Touring Sedan', 'Vintage Cars', '1:18', 'Welly Diecast Productions', 'Features wooden spoke wheels, brass headlights and fold-down windshield.', 2724, 86.70, 170.00),
('S24_3856', '1956 Porsche 356A Coupe', 'Classic Cars', '1:18', 'Classic Metal Creations', 'Precision diecast with opening engine lid, detailed air-cooled flat-4.', 6600, 98.30, 140.43),
('S700_1938', 'The Mayflower', 'Ships', '1:700', 'Studio M Art Models', 'All wood construction with canvas sails and rigging.', 737, 43.30, 86.61);

INSERT INTO customers (customerNumber, customerName, contactLastName, contactFirstName, phone, addressLine1, addressLine2, city, state, postalCode, country, salesRepEmployeeNumber, creditLimit) VALUES
(103, 'Atelier graphique', 'Schmitt', 'Carine', '40.32.2555', '54, rue Royale', NULL, 'Nantes', NULL, '44000', 'France', 1370, 21000.00),
(112, 'Signal Gift Stores', 'King', 'Jean', '7025551838', '8489 Strong St.', NULL, 'Las Vegas', 'NV', '83030', 'USA', 1166, 71800.00),
(114, 'Australian Collectors, Co.', 'Ferguson', 'Peter', '03 9520 4555', '636 St Kilda Road', 'Level 3', 'Melbourne', 'Victoria', '3004', 'Australia', 1611, 117300.00),
(119, 'La Rochelle Gifts', 'Labrune', 'Janine', '40.67.8555', '67, rue des Cinquante Otages', NULL, 'Nantes', NULL, '44000', 'France', 1370, 118200.00),
(121, 'Baane Mini Imports', 'Bergulfsen', 'Jonas', '07-98 9555', 'Erling Skakkes gate 78', NULL, 'Stavern', NULL, '4110', 'Norway', 1501, 81700.00),
(124, 'Mini Gifts Distributors Ltd.', 'Nelson', 'Susan', '4155551450', '5677 Strong St.', NULL, 'San Rafael', 'CA', '97562', 'USA', 1165, 210500.00),
(128, 'Blauer See Auto, Co.', 'Keitel', 'Roland', '+49 69 66 90 2555', 'Lyonerstr. 34', NULL, 'Frankfurt', NULL, '60528', 'Germany', 1501, 59700.00);

INSERT INTO orders (orderNumber, orderDate, requiredDate, shippedDate, status, comments, customerNumber) VALUES
(10100, '2024-01-06', '2024-01-13', '2024-01-10', 'Shipped', NULL, 114),
(10101, '2024-01-09', '2024-01-18', '2024-01-11', 'Shipped', 'Check on delivery address', 128),
(10102, '2024-01-10', '2024-01-18', '2024-01-14', 'Shipped', NULL, 103),
(10103, '2024-01-29', '2024-02-07', '2024-02-02', 'Shipped', NULL, 121),
(10104, '2024-01-31', '2024-02-09', '2024-02-05', 'Resolved', 'Resolved payment dispute', 112),
(10105, '2024-02-05', '2024-02-13', NULL, 'In Process', 'Pending warehouse packing', 124);

INSERT INTO orderdetails (orderNumber, productCode, quantityOrdered, priceEach, orderLineNumber) VALUES
(10100, 'S18_1749', 30, 136.00, 3),
(10100, 'S24_3856', 50, 112.34, 1),
(10100, 'S700_1938', 22, 69.29, 2),
(10101, 'S10_1678', 25, 76.56, 1),
(10101, 'S10_1949', 26, 171.44, 2),
(10102, 'S10_2016', 39, 95.15, 2),
(10102, 'S18_1749', 41, 136.00, 1),
(10103, 'S10_1949', 26, 171.44, 1),
(10104, 'S12_1099', 34, 155.66, 1),
(10105, 'S10_1678', 45, 76.56, 1);

INSERT INTO payments (customerNumber, checkNumber, paymentDate, amount) VALUES
(103, 'HQ336336', '2024-01-16', 14571.44),
(112, 'ND439363', '2024-02-10', 5292.44),
(114, 'GG31455', '2024-01-15', 10549.01),
(121, 'DB933704', '2024-02-05', 4457.44),
(124, 'CQ287955', '2024-02-15', 3445.20),
(128, 'DI925118', '2024-01-20', 6372.48);
