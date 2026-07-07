using System.Collections.Generic;
using System.Globalization;
using System.IO;
using CsvHelper;

namespace CsvHelperDemo
{
    // Deliberately has NO [Name]/[Index] attributes and no explicit
    // ClassMap anywhere in this file — every property is mapped purely by
    // CsvHelper's own AutoMap() reflection, matched against the CSV
    // header by property name.
    public class Product
    {
        public string Name { get; set; }
        public int Quantity { get; set; }
        public double Price { get; set; }
        public bool InStock { get; set; }
    }

    public class CsvHelperDemoRunner
    {
        // Reads csvText into a List<Product> using CsvReader.GetRecords<T>()
        // with no ClassMap registered at all — this exercises CsvHelper's
        // real AutoMap() path: CsvContext.AutoMap(Type) building a closed
        // DefaultClassMap<Product> via reflection (Type.GetConstructor(s),
        // Expression.New/Lambda/Compile), then a compiled Expression-tree
        // delegate reading each column by name and converting it through
        // CsvHelper's own real TypeConverters.
        public static List<Product> ReadProducts(string csvText)
        {
            using (var reader = new StringReader(csvText))
            using (var csv = new CsvReader(reader, CultureInfo.InvariantCulture))
            {
                var products = new List<Product>();
                foreach (var product in csv.GetRecords<Product>())
                {
                    products.Add(product);
                }
                return products;
            }
        }
    }
}
