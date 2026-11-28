namespace App;

public sealed partial class MainPage
{
    void Paint()
    {
        var accent = Application.Current.Resources["DesignAccent"];
        var system = Resources["SystemAccentColor"];
        var typo = Resources["DesignNope"];
    }
}
