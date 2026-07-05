using System;
using System.Reflection;

namespace Vmnet.Fixtures
{
    // Fase 3.63 golden fixture: real System.Reflection.CustomAttributeData/
    // CustomAttributeExtensions.GetCustomAttribute<T> support — a genuinely
    // new subsystem (real attribute-blob decoding, ECMA-335 §II.23.3),
    // deliberately deferred until several real Microsoft.Extensions.*
    // packages confirmed real demand for it (Configuration.Binder's own
    // [ConfigurationKeyName] property attribute, most directly).
    public class SimpleNameAttribute : Attribute
    {
        public string Name;

        public SimpleNameAttribute(string name)
        {
            Name = name;
        }
    }

    [SimpleName("ClassLevel")]
    public class CustomAttributeTarget
    {
        [SimpleName("PropLevel")]
        public string TaggedProperty { get; set; }

        public string UntaggedProperty { get; set; }
    }

    public static class CustomAttributeTest
    {
        // Exercises the low-level CustomAttributeData API directly —
        // the exact shape Microsoft.Extensions.Configuration.Binder's own
        // real GetPropertyName uses to read [ConfigurationKeyName].
        public static string ReadPropertyAttributeViaData()
        {
            PropertyInfo prop = typeof(CustomAttributeTarget).GetProperty("TaggedProperty");
            var datas = prop.GetCustomAttributesData();
            if (datas.Count == 0)
            {
                return "NONE";
            }
            CustomAttributeData data = datas[0];
            var args = data.ConstructorArguments;
            return data.AttributeType.Name + ":" + args[0].Value;
        }

        // Exercises the high-level, real-instance-constructing API —
        // the shape Markdig's own Markdown.Version (and most real-world
        // code) actually uses.
        public static string ReadPropertyAttributeViaGeneric()
        {
            PropertyInfo prop = typeof(CustomAttributeTarget).GetProperty("TaggedProperty");
            SimpleNameAttribute attr = prop.GetCustomAttribute<SimpleNameAttribute>();
            if (attr == null)
            {
                return "NULL";
            }
            return attr.Name;
        }

        public static string ReadTypeAttribute()
        {
            SimpleNameAttribute attr = typeof(CustomAttributeTarget).GetCustomAttribute<SimpleNameAttribute>();
            if (attr == null)
            {
                return "NULL";
            }
            return attr.Name;
        }

        public static bool UntaggedPropertyHasNoAttribute()
        {
            PropertyInfo prop = typeof(CustomAttributeTarget).GetProperty("UntaggedProperty");
            return prop.GetCustomAttributesData().Count == 0;
        }

        public static bool UntaggedPropertyIsDefinedIsFalse()
        {
            PropertyInfo prop = typeof(CustomAttributeTarget).GetProperty("UntaggedProperty");
            return prop.IsDefined(typeof(SimpleNameAttribute), false);
        }

        public static bool TaggedPropertyIsDefinedIsTrue()
        {
            PropertyInfo prop = typeof(CustomAttributeTarget).GetProperty("TaggedProperty");
            return prop.IsDefined(typeof(SimpleNameAttribute), false);
        }
    }
}
